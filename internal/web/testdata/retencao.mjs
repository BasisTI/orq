// Teste de navegador da US #41, r2: o /vivo não troca nem remove a pendência que a pessoa está
// editando (formulário aberto ou texto digitado), marca como desatualizada e aplica a mudança
// quando ela fecha o item. Casos B (outra pessoa responde), C (coordenador resolve) e D ("Trocar a
// resposta" em digitação, coordenador resolve), partindo do texto digitado.
// Uso: node retencao.mjs <base-url> <controle>; env: ORQ_PLAYWRIGHT, ORQ_CHROMIUM.
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';

const { chromium } = await import(pathToFileURL(join(process.env.ORQ_PLAYWRIGHT, 'index.mjs')).href);
const [base, controle] = process.argv.slice(2);
const problemas = [];
const falha = (m) => problemas.push(m);
const muda = async (a) => { const r = await fetch(`${controle}/${a}`); if (!r.ok) falha(`controle ${a}: ${r.status}`); };
const nav = await chromium.launch({ executablePath: process.env.ORQ_CHROMIUM, args: ['--no-sandbox'] });
const ctx = await nav.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' });
const p = await ctx.newPage();
const consoles = [];
p.on('console', (m) => { if (m.type() === 'error' || m.type() === 'warning') consoles.push(`${m.type()}: ${m.text()}`); });
p.on('pageerror', (e) => consoles.push(`pageerror: ${e.message}`));
await p.clock.install();
await p.goto(base + '/', { waitUntil: 'networkidle' });
const ciclo = async () => { await Promise.all([p.waitForResponse((r) => r.url().includes('/vivo')), p.clock.runFor(30500)]); await p.waitForTimeout(300); };
const abreUS = (id) => p.evaluate((a) => { const u = document.getElementById(a); u.closest('details.onda').open = true; u.open = true; }, id);
const abreForm = async (pid, texto) => {
  await p.click(`#${pid} > summary`);
  await p.waitForSelector(`#${pid} textarea`, { timeout: 5000 });
  await p.fill(`#${pid} textarea`, texto);
};
const foto = (pid) => p.evaluate((a) => {
  const d = document.getElementById(a);
  return { existe: !!d, aberto: d?.open ?? null, estado: d?.dataset.estado ?? null, texto: d?.querySelector('textarea')?.value ?? null, desatualizada: d?.dataset.desatualizada ?? null, aviso: d?.querySelector('[data-aviso-desatualizada]')?.textContent ?? null };
}, pid);
const conta = (us) => p.evaluate((a) => document.querySelector(`#${a} .conta-wrap`)?.textContent.trim() ?? '', us);

// Contagem e lista concordam em TODAS as US; o item retido (data-desatualizada) é a exceção
// explícita e precisa do selo visível no <summary>. "Nenhuma pendência." aparece se e só se a lista
// está vazia (o retido conta como item).
const invariante = async (rot) => {
  const r = await p.evaluate(() => {
    const ruins = [];
    for (const u of document.querySelectorAll('details.us')) {
      const n = parseInt(u.querySelector('.conta-wrap')?.textContent.trim() || '0', 10);
      const lista = [...u.querySelectorAll('.pend-lista-us > details.pend')];
      const retidas = lista.filter((d) => d.dataset.desatualizada);
      const ab = lista.filter((d) => d.dataset.estado === 'aberta' && !d.dataset.desatualizada).length;
      const vz = u.querySelector('[id$="-vazio"]');
      const vzVisivel = !!vz && !vz.hidden && getComputedStyle(vz).display !== 'none';
      if (retidas.length === 0 && n !== ab) ruins.push({ us: u.id, motivo: 'contagem', titulo: n, abertas: ab });
      if (vzVisivel !== (lista.length === 0)) ruins.push({ us: u.id, motivo: 'vazio', itens: lista.length, vzVisivel });
      for (const d of retidas) {
        const m = d.querySelector(':scope > summary [data-marca-desatualizada]');
        const r = m?.getBoundingClientRect();
        if (!m || !r.width || !r.height) ruins.push({ us: u.id, motivo: 'sem selo visível', item: d.id, aberto: d.open });
      }
    }
    const ids = [...document.querySelectorAll('[id^="pend-"]')].map((e) => e.id);
    return { ruins, dup: ids.filter((x, i) => ids.indexOf(x) !== i) };
  });
  if (r.ruins.length || r.dup.length) falha(`invariante (${rot}): ${JSON.stringify(r)}`);
};
const retido = (rot, f, estadoNovo, texto) => {
  if (!f.existe) return falha(`${rot}: o item sumiu`);
  if (!f.aberto) falha(`${rot}: o item foi fechado`);
  if (f.texto !== texto) falha(`${rot}: texto = ${JSON.stringify(f.texto)}, quer ${JSON.stringify(texto)}`);
  if (f.desatualizada !== estadoNovo) falha(`${rot}: marca = ${JSON.stringify(f.desatualizada)}, quer ${estadoNovo}`);
  if (!f.aviso?.includes('depois que você a abriu')) falha(`${rot}: sem o aviso de desatualizada (${JSON.stringify(f.aviso)})`);
};
// Fecha o item (clique no summary) e confere que a mudança é aplicada na hora, sem esperar 30 s.
const fechaEAplica = async (rot, pid, aplicado) => {
  await p.click(`#${pid} > summary`);
  try {
    await p.waitForFunction(aplicado, pid, { timeout: 5000 });
  } catch { falha(`${rot}: a mudança não foi aplicada ao fechar o item`); }
};

// B) Formulário aberto com texto; outra pessoa responde.
await abreUS('us-catalogo-404');
await abreForm('pend-catalogo-404-2', 'B texto longo que levei minutos escrevendo');
const cB = await conta('us-catalogo-404');
await muda('responder?proj=catalogo&ref=404&n=2');
await ciclo(); await invariante('B');
retido('B', await foto('pend-catalogo-404-2'), 'respondida', 'B texto longo que levei minutos escrevendo');
if (await conta('us-catalogo-404') === cB) falha('B: a contagem do título não acompanhou o servidor');
// Outro ciclo com o item ainda aberto: continua retido, sem duplicar o aviso.
await ciclo();
retido('B (2º ciclo)', await foto('pend-catalogo-404-2'), 'respondida', 'B texto longo que levei minutos escrevendo');
if (await p.locator('#pend-catalogo-404-2 [data-aviso-desatualizada]').count() !== 1) falha('B: aviso duplicado');
// Fechado com texto digitado: continua retido (o texto não se perde).
await p.click('#pend-catalogo-404-2 > summary');
await ciclo();
{ const f = await foto('pend-catalogo-404-2'); if (!f.existe || f.estado !== 'aberta' || f.texto !== 'B texto longo que levei minutos escrevendo') falha(`B fechado com texto: ${JSON.stringify(f)}`); }
await invariante('T2 B fechado com texto (selo no summary)');
// Apaga o texto e fecha: a mudança é aplicada.
await p.click('#pend-catalogo-404-2 > summary');
await p.fill('#pend-catalogo-404-2 textarea', '');
await fechaEAplica('B', 'pend-catalogo-404-2', (id) => document.getElementById(id)?.dataset.estado === 'respondida' && !document.getElementById(id).dataset.desatualizada);
await invariante('B depois');

// C) Formulário aberto com texto; o coordenador resolve.
await abreUS('us-faturamento-513');
await abreForm('pend-faturamento-513-1', 'C texto digitado');
await muda('resolver?proj=faturamento&ref=513&n=1');
await ciclo(); await invariante('C');
retido('C', await foto('pend-faturamento-513-1'), 'resolvida', 'C texto digitado');
await p.fill('#pend-faturamento-513-1 textarea', '');
await fechaEAplica('C', 'pend-faturamento-513-1', (id) => !document.getElementById(id));
await invariante('C depois');
if (!(await p.evaluate(() => !document.getElementById('us-faturamento-513-vazio').hidden))) falha('C: "Nenhuma pendência." não voltou depois de removida');

// D) "Trocar a resposta" com texto; o coordenador resolve.
await abreUS('us-catalogo-404');
await p.click('#pend-catalogo-404-1 > summary');
await p.waitForSelector('#pend-catalogo-404-1 button:has-text("Trocar a resposta")', { timeout: 5000 });
await p.click('#pend-catalogo-404-1 button:has-text("Trocar a resposta")');
await p.waitForSelector('#pend-catalogo-404-1 textarea', { timeout: 5000 });
await p.fill('#pend-catalogo-404-1 textarea', 'D nova resposta');
await muda('resolver?proj=catalogo&ref=404&n=1');
await ciclo(); await invariante('D');
retido('D', await foto('pend-catalogo-404-1'), 'resolvida', 'D nova resposta');

// E) (T4) Enviar a resposta de um item retido já resolvido: o servidor recusa (409), o erro aparece
//    no formulário, o texto fica, o item segue retido com o aviso do caso resolvido; apagar o texto
//    e fechar aplica a remoção.
{
  const f0 = await foto('pend-catalogo-404-1');
  if (!f0.aviso?.includes('já não aceita resposta')) falha(`E: o aviso do caso resolvido não o diz: ${JSON.stringify(f0.aviso)}`);
  await p.click('#pend-catalogo-404-1 button[type=submit]');
  try { await p.waitForFunction(() => document.querySelector('#pend-catalogo-404-1 .erro')?.textContent.trim(), null, { timeout: 5000 }); } catch { falha('E: o 409 não apareceu no formulário'); }
  retido('E (depois do 409)', await foto('pend-catalogo-404-1'), 'resolvida', 'D nova resposta');
  await invariante('E');
  await p.fill('#pend-catalogo-404-1 textarea', '');
  await fechaEAplica('E', 'pend-catalogo-404-1', (id) => !document.getElementById(id));
  await invariante('E depois');
}

// T1) A única pendência da US, com formulário aberto, é resolvida: o item fica na lista e
//     "Nenhuma pendência." não aparece (feito no C acima: invariante); aqui, o vazio num item sem
//     edição some e volta com a remoção (pend de 'faturamento' já cobriu).

// T3a) Decisão com opção marcada e o item fechado: a escolha não retém; a mudança entra no ciclo.
await abreUS('us-nuvem-78');
await p.click('#pend-nuvem-78-2 > summary');
await p.waitForSelector('#pend-nuvem-78-2 input[type=radio]', { timeout: 5000 });
await p.check('#pend-nuvem-78-2 input[type=radio] >> nth=0');
await p.click('#pend-nuvem-78-2 > summary');
await muda('resolver?proj=nuvem&ref=78&n=2');
await ciclo(); await invariante('T3a');
if ((await foto('pend-nuvem-78-2')).existe) falha('T3a: a opção marcada com o item fechado reteve o item');

// T3b) A mesma escolha com o item aberto retém (a pessoa está nele); fechar libera sem texto.
await p.click('#pend-nuvem-78-1 > summary');
await p.waitForSelector('#pend-nuvem-78-1 input[type=radio]', { timeout: 5000 });
await p.check('#pend-nuvem-78-1 input[type=radio] >> nth=0');
await muda('resolver?proj=nuvem&ref=78&n=1');
await ciclo(); await invariante('T3b');
{ const f = await foto('pend-nuvem-78-1'); if (!f.existe || !f.aberto || f.desatualizada !== 'resolvida') falha(`T3b: item aberto com opção devia ficar retido: ${JSON.stringify(f)}`); }
await fechaEAplica('T3b', 'pend-nuvem-78-1', (id) => !document.getElementById(id));
await invariante('T3b depois');

// O 409 do caso E é provocado de propósito: o navegador e o htmx o registram como erro de console.
for (const c of consoles) if (!/409|Response Status Error Code/.test(c)) falha(c);
await nav.close();
console.log(JSON.stringify({ problemas }));
process.exit(problemas.length ? 1 : 0);
