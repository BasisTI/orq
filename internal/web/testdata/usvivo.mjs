// Teste de navegador da US #41: o /vivo atualiza, sem recarregar, o status da US, o status do
// Taiga (corpo da US) e a lista de pendências, inclusive a que parte de "Nenhuma pendência.";
// a contagem do título concorda com a lista; o que a pessoa digitou não se perde.
// Uso: node usvivo.mjs <base-url> <controle>; env: ORQ_PLAYWRIGHT, ORQ_CHROMIUM.
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';

const { chromium } = await import(pathToFileURL(join(process.env.ORQ_PLAYWRIGHT, 'index.mjs')).href);
const [base, controle] = process.argv.slice(2);
const problemas = [];
const falha = (m) => problemas.push(m);
const muda = async (acao) => { const r = await fetch(`${controle}/${acao}`); if (!r.ok) falha(`controle ${acao}: ${r.status}`); };

const navegador = await chromium.launch({ executablePath: process.env.ORQ_CHROMIUM, args: ['--no-sandbox'] });
const ctx = await navegador.newContext({ viewport: { width: 1440, height: 900 }, reducedMotion: 'reduce' });
const pagina = await ctx.newPage();
const consoles = [];
pagina.on('console', (m) => { if (m.type() === 'error' || m.type() === 'warning') consoles.push(`console.${m.type()}: ${m.text()}`); });
pagina.on('pageerror', (e) => consoles.push(`pageerror: ${e.message}`));
await pagina.clock.install();
await pagina.goto(base + '/', { waitUntil: 'networkidle' });
const ciclo = async () => {
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/vivo')), pagina.clock.runFor(30500)]);
  await pagina.waitForTimeout(300);
};

// catalogo/406: exec, "In progress", sem pendência. catalogo/405: rev, com 1 pendência aberta.
const vista = (anchor) => pagina.evaluate((a) => {
  const us = document.getElementById(a);
  const itens = [...us.querySelectorAll('.pend-lista-us > details.pend')].map((d) => d.id);
  const vazio = us.querySelector('.vazio-linha[id$="-vazio"]');
  const conta = us.querySelector('.conta-wrap')?.textContent.replace(/\s+/g, ' ').trim() ?? '';
  return {
    status: us.querySelector('.us-selos .selo')?.textContent.trim(),
    taiga: us.querySelector('.us-dl dd')?.textContent.replace(/\s+/g, ' ').trim(),
    itens,
    vazioVisivel: !!vazio && !vazio.hidden && getComputedStyle(vazio).display !== 'none',
    conta,
    aberta: us.open,
  };
}, anchor);

await pagina.evaluate(() => {
  for (const id of ['us-catalogo-406', 'us-catalogo-405']) { const u = document.getElementById(id); u.closest('details.onda').open = true; u.open = true; }
  const t = document.createElement('textarea'); t.id = 'teste-texto';
  document.querySelector('#us-catalogo-405 .us-corpo').appendChild(t); t.value = 'digitado';
});

const a0 = await vista('us-catalogo-406');
if (a0.itens.length !== 0 || !a0.vazioVisivel) falha(`preparo: 406 deveria partir de "Nenhuma pendência." (${JSON.stringify(a0)})`);

// 1) Pendência nova, partindo do vazio: a contagem do título e a lista concordam sem recarregar.
await muda('nova-pend?proj=catalogo&ref=406');
await ciclo();
const a1 = await vista('us-catalogo-406');
if (!a1.conta.startsWith('1 espera você')) falha(`título sem a pendência nova: ${JSON.stringify(a1.conta)}`);
if (a1.itens.length !== 1) falha(`lista sem a pendência nova (título diz "${a1.conta}", lista tem ${a1.itens.length}): ${JSON.stringify(a1)}`);
if (a1.vazioVisivel) falha('"Nenhuma pendência." continua visível com 1 pendência');

// 2) Estado da US e status do Taiga mudam: aparecem sem recarregar.
await muda('status?proj=catalogo&ref=406&status=rev');
await muda('taiga?proj=catalogo&ref=406&status=Ready%20for%20test');
await ciclo();
const a2 = await vista('us-catalogo-406');
if (a2.status !== 'revisão') falha(`estado da US = ${JSON.stringify(a2.status)}, quer "revisão"`);
if (!a2.taiga.includes('Ready for test')) falha(`status do Taiga = ${JSON.stringify(a2.taiga)}, quer "Ready for test"`);

// 3) Pendência respondida por outra pessoa: o item muda de estado e a contagem cai.
await muda('responder?proj=catalogo&ref=406&n=1');
await ciclo();
const a3 = await vista('us-catalogo-406');
if (a3.conta !== '') falha(`título ainda mostra espera depois da resposta: ${JSON.stringify(a3.conta)}`);
const marca = await pagina.evaluate(() => document.querySelector('#us-catalogo-406 .pend-lista-us > details.pend')?.dataset.estado);
if (marca !== 'respondida') falha(`item da pendência respondida fora do lugar (estado ${marca})`);

// 4) Resolvida: sai da lista; sem pendência, volta "Nenhuma pendência.".
await muda('resolver?proj=catalogo&ref=406&n=1');
await ciclo();
const a4 = await vista('us-catalogo-406');
if (a4.itens.length !== 0 || !a4.vazioVisivel) falha(`depois de resolvida: ${JSON.stringify(a4)}`);

// 5) O que estava aberto e digitado na 405 continua.
const d = await pagina.evaluate(() => ({ t: document.querySelector('#teste-texto')?.value, ab: document.getElementById('us-catalogo-405').open }));
if (d.t !== 'digitado' || !d.ab) falha(`o /vivo mexeu no que estava aberto/digitado: ${JSON.stringify(d)}`);

// 6) Pendência nova na 405 (que já tinha uma): entra na lista sem tocar na que existia.
await pagina.evaluate(() => { document.querySelector('#us-catalogo-405 .pend-lista-us > details.pend').open = true; });
await muda('nova-pend?proj=catalogo&ref=405');
await ciclo();
const a6 = await vista('us-catalogo-405');
if (a6.itens.length !== 2 || !a6.conta.startsWith('2 esperam você')) falha(`405 com 2 pendências: ${JSON.stringify(a6)}`);
const aberto = await pagina.evaluate(() => document.querySelector('#us-catalogo-405 .pend-lista-us > details.pend').open);
if (!aberto) falha('a pendência que a pessoa abriu foi fechada pelo /vivo');

for (const c of consoles) falha(c);
await navegador.close();
console.log(JSON.stringify({ problemas }));
process.exit(problemas.length ? 1 : 0);
