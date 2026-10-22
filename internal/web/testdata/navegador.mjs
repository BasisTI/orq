// Teste de navegador da web do orq (go test -tags=browser). Abre cada página no Chromium e
// falha em qualquer violação de CSP e em qualquer erro de console fora da lista de permissões
// (spec §8.1, S6). Uso: node navegador.mjs <base-url> <caminho>... ; env: ORQ_PLAYWRIGHT (pasta
// do playwright-core), ORQ_CHROMIUM (executável), ORQ_CAPTURAS (pasta das capturas, opcional).
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { mkdirSync } from 'node:fs';

const { chromium } = await import(pathToFileURL(join(process.env.ORQ_PLAYWRIGHT, 'index.mjs')).href);
const [base, ...caminhos] = process.argv.slice(2);
const capturas = process.env.ORQ_CAPTURAS;
if (capturas) mkdirSync(capturas, { recursive: true });

// Os dois console.error de cada 4xx provocado de propósito (spec §5.1, S6). Nesta story nenhuma
// página provoca 4xx, então a lista fica vazia de pedidos permitidos.
const PERMITIDOS = [];

const navegador = await chromium.launch({ executablePath: process.env.ORQ_CHROMIUM, args: ['--no-sandbox'] });
const problemas = [];
const resumo = [];

for (const [nome, largura, altura, escala] of [['390', 390, 844, 2], ['1440', 1440, 900, 1]]) {
  const ctx = await navegador.newContext({ viewport: { width: largura, height: altura }, deviceScaleFactor: escala, reducedMotion: 'reduce' });
  for (const caminho of caminhos) {
    const pagina = await ctx.newPage();
    const aqui = `${caminho}@${nome}`;
    const falhas = [];
    pagina.on('console', (m) => {
      if (m.type() !== 'error' && m.type() !== 'warning') return;
      const texto = m.text();
      if (PERMITIDOS.some((p) => texto.includes(p))) return;
      falhas.push(`console.${m.type()}: ${texto}`);
    });
    pagina.on('pageerror', (e) => falhas.push(`pageerror: ${e.message}`));
    pagina.on('requestfailed', (r) => falhas.push(`requestfailed: ${r.url()} ${r.failure()?.errorText}`));
    pagina.on('response', (r) => {
      if (r.status() >= 400 && !PERMITIDOS.length) falhas.push(`HTTP ${r.status()}: ${r.url()}`);
    });
    await pagina.addInitScript(() => {
      window.__csp = [];
      document.addEventListener('securitypolicyviolation', (e) => window.__csp.push(`${e.violatedDirective} ${e.blockedURI}`));
    });
    const resp = await pagina.goto(base + caminho, { waitUntil: 'networkidle' });
    if (resp.status() !== 200) falhas.push(`status ${resp.status()}`);
    await pagina.evaluate(() => document.fonts.ready);

    const estado = await pagina.evaluate(() => ({
      csp: window.__csp,
      comStyle: document.querySelectorAll('[style]').length,
      scriptsInline: [...document.scripts].filter((s) => !s.src).length,
      htmx: typeof window.htmx,
      allowEval: window.htmx?.config?.allowEval,
      historyRestoreAsHxRequest: window.htmx?.config?.historyRestoreAsHxRequest,
      historyCacheSize: window.htmx?.config?.historyCacheSize,
      carlito: document.fonts.check('16px Carlito') && [...document.fonts].some((f) => f.family.replace(/"/g, '') === 'Carlito' && f.status === 'loaded'),
      interTight: [...document.fonts].some((f) => f.family.replace(/"/g, '') === 'Inter Tight' && f.status === 'loaded'),
      fundoCabecalho: (document.querySelector('.cab') ? getComputedStyle(document.querySelector('.cab')).backgroundColor : null),
      abaAtiva: document.querySelector('.abas-in a[aria-current="page"]')?.textContent.trim() ?? null,
      icone: document.querySelector('link[rel=icon]')?.href,
      larguraRolavel: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    }));
    if (estado.csp.length) falhas.push(`violação de CSP: ${estado.csp.join('; ')}`);
    if (estado.comStyle) falhas.push(`${estado.comStyle} elemento(s) com style=`);
    if (estado.scriptsInline) falhas.push(`${estado.scriptsInline} <script> inline`);
    if (estado.htmx !== 'object' || estado.allowEval !== false || estado.historyRestoreAsHxRequest !== false || estado.historyCacheSize !== 0) {
      falhas.push(`htmx/meta: ${JSON.stringify({ htmx: estado.htmx, allowEval: estado.allowEval, h: estado.historyRestoreAsHxRequest, c: estado.historyCacheSize })}`);
    }
    if (!estado.carlito || !estado.interTight) falhas.push(`fontes não carregadas: ${JSON.stringify({ c: estado.carlito, i: estado.interTight })}`);
    if (estado.fundoCabecalho !== 'rgb(7, 26, 46)') falhas.push(`cabeçalho sem o navy do tema: ${estado.fundoCabecalho}`);
    if (estado.larguraRolavel > 0) falhas.push(`rolagem horizontal da página: ${estado.larguraRolavel}px`);

    if (capturas) {
      const slug = caminho.replace(/[^a-z0-9]+/gi, '_').replace(/^_|_$/g, '') || 'raiz';
      if (caminho === '/') {
        await pagina.screenshot({ path: join(capturas, `cabecalho-abas-${nome}.png`), clip: { x: 0, y: 0, width: largura, height: 150 } });
      }
      await pagina.screenshot({ path: join(capturas, `pagina-${slug}-${nome}.png`), fullPage: true });
    }
    resumo.push({ aqui, abaAtiva: estado.abaAtiva, falhas });
    for (const f of falhas) problemas.push(`${aqui}: ${f}`);
    await pagina.close();
  }
  await ctx.close();
}
await navegador.close();
console.log(JSON.stringify({ resumo, problemas }));
process.exit(problemas.length ? 1 : 0);
