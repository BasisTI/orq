// Teste de navegador do /vivo (go test -tags=browser; spec §6 e §8.1): a atualização de 30 s não
// apaga a página, não fecha o que está aberto nem apaga o que foi digitado, conta as mudanças e,
// com a lista diferente da que a página tem, avisa em vez de mexer. Uso: node vivo.mjs <base-url>;
// env: ORQ_PLAYWRIGHT (pasta do playwright-core), ORQ_CHROMIUM (executável).
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';

const { chromium } = await import(pathToFileURL(join(process.env.ORQ_PLAYWRIGHT, 'index.mjs')).href);
const [base] = process.argv.slice(2);

const navegador = await chromium.launch({ executablePath: process.env.ORQ_CHROMIUM, args: ['--no-sandbox'] });
const problemas = [];
const falha = (m) => problemas.push(m);

const ctx = await navegador.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' });
const pagina = await ctx.newPage();
const consoles = [];
pagina.on('console', (m) => { if (m.type() === 'error' || m.type() === 'warning') consoles.push(`console.${m.type()}: ${m.text()}`); });
pagina.on('pageerror', (e) => consoles.push(`pageerror: ${e.message}`));
const pedidos = [];
pagina.on('request', (r) => { if (r.url().includes('/vivo')) pedidos.push(r.url()); });
const respostasVivo = [];
pagina.on('response', async (r) => { if (r.url().includes('/vivo')) respostasVivo.push(r.status()); });

await pagina.clock.install();
await pagina.goto(base + '/central', { waitUntil: 'networkidle' });

const foto = () => pagina.evaluate(() => ({
  filhosDoBody: document.body.children.length,
  ondasAbertas: [...document.querySelectorAll('details.onda[open]')].map((d) => d.id),
  usAbertas: [...document.querySelectorAll('details.us[open]')].map((d) => d.id),
  texto: document.querySelector('#teste-texto')?.value ?? null,
  pendAba: document.querySelector('#aba-n-pendencias')?.textContent.trim(),
  situacao: document.querySelector('#situacao')?.textContent.replace(/\s+/g, ' ').trim(),
  hxGet: document.querySelector('#vivo')?.getAttribute('hx-get'),
  vivos: document.querySelectorAll('#vivo').length,
  filhosDoVivo: document.querySelector('#vivo')?.children.length,
  formularios: document.querySelectorAll('form, textarea:not(#teste-texto), input').length,
  aviso: document.querySelector('#aviso-lista')?.textContent.trim(),
}));

// Abre a primeira onda e a primeira US, e digita num campo dentro dela.
await pagina.evaluate(() => {
  const onda = document.querySelector('details.onda');
  onda.open = true;
  const us = onda.querySelector('details.us');
  us.open = true;
  const t = document.createElement('textarea');
  t.id = 'teste-texto';
  us.querySelector('.us-corpo').appendChild(t);
  t.value = 'texto digitado pela pessoa';
});
const antes = await foto();
if (antes.ondasAbertas.length !== 1 || antes.usAbertas.length !== 1) falha(`preparo: ondas abertas ${antes.ondasAbertas}, US abertas ${antes.usAbertas}`);

// Muda o estado no servidor, pelo caminho real da resposta (POST do formulário da pendência).
const status = await pagina.evaluate(async () => {
  const r = await fetch('/p/catalogo/404/2/responder', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'texto=ok', redirect: 'manual' });
  return r.status === 0 ? 303 : r.status;
});
if (status !== 303) falha(`POST de resposta = ${status}, quer 303`);

// Um ciclo de 30 s.
await Promise.all([pagina.waitForResponse((r) => r.url().includes('/vivo')), pagina.clock.runFor(30500)]);
await pagina.waitForTimeout(300);
const depois = await foto();

if (depois.filhosDoBody !== antes.filhosDoBody) falha(`o /vivo mexeu no <body>: ${antes.filhosDoBody} -> ${depois.filhosDoBody} filhos (a página sumiu?)`);
if (JSON.stringify(depois.ondasAbertas) !== JSON.stringify(antes.ondasAbertas) || JSON.stringify(depois.usAbertas) !== JSON.stringify(antes.usAbertas)) falha(`o /vivo fechou o que estava aberto: ${JSON.stringify(antes.ondasAbertas)}/${JSON.stringify(antes.usAbertas)} -> ${JSON.stringify(depois.ondasAbertas)}/${JSON.stringify(depois.usAbertas)}`);
if (depois.texto !== 'texto digitado pela pessoa') falha(`o /vivo apagou o que foi digitado: ${JSON.stringify(depois.texto)}`);
if (Number(depois.pendAba) !== Number(antes.pendAba) - 1) falha(`a contagem da aba Pendências não caiu 1: ${antes.pendAba} -> ${depois.pendAba}`);
if (depois.situacao === antes.situacao) falha('a linha de situação não mudou depois da resposta');
if (depois.vivos !== 1 || depois.filhosDoVivo !== 0) falha(`#vivo: ${depois.vivos} elemento(s), ${depois.filhosDoVivo} filho(s)`);
if (depois.formularios !== 0) falha(`o /vivo trouxe ${depois.formularios} formulário(s)/campo(s) para a página`);
if (depois.aviso) falha(`aviso de lista sem a lista ter mudado: ${depois.aviso}`);
if (pedidos.length !== 1) falha(`pedidos ao /vivo no primeiro ciclo = ${pedidos.length}, quer 1`);

// Segundo ciclo: o #vivo foi rearmado pelo oob e continua pedindo, com o mesmo hash.
await Promise.all([pagina.waitForResponse((r) => r.url().includes('/vivo')), pagina.clock.runFor(30500)]);
await pagina.waitForTimeout(200);
if (pedidos.length !== 2) falha(`pedidos ao /vivo depois de dois ciclos = ${pedidos.length}, quer 2`);
if (pedidos.length === 2 && pedidos[0] !== pedidos[1]) falha(`o v mudou entre os ciclos: ${pedidos.join(' | ')}`);

// Hash diferente do que a página tem: avisa e não mexe em nada.
await pagina.evaluate(() => {
  const v = document.querySelector('#vivo');
  v.setAttribute('hx-get', '/vivo?v=000000000000');
  htmx.process(v); // o htmx lê o hx-get ao processar o elemento, não a cada ciclo
});
const antesDoAviso = await foto();
await Promise.all([pagina.waitForResponse((r) => r.url().includes('/vivo')), pagina.clock.runFor(30500)]);
await pagina.waitForTimeout(300);
const comAviso = await foto();
if (!comAviso.aviso?.startsWith('A lista mudou')) falha(`sem o aviso "A lista mudou": ${JSON.stringify(comAviso.aviso)}`);
if (comAviso.filhosDoBody !== antesDoAviso.filhosDoBody || comAviso.texto !== 'texto digitado pela pessoa' || comAviso.usAbertas.length !== 1) falha('o aviso de lista mexeu na página');

// Um 503 num ciclo não troca nada (hx-swap="none") e o poll continua: servidor de teste não tem
// como falhar sob demanda; o httptest cobre o status. Aqui só se confere que não houve erro de console.
for (const c of consoles) falha(c);
for (const r of respostasVivo) if (r !== 200) falha(`/vivo respondeu ${r}`);

await navegador.close();
console.log(JSON.stringify({ problemas }));
process.exit(problemas.length ? 1 : 0);
