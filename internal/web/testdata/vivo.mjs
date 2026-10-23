// Teste de navegador do /vivo (go test -tags=browser; spec §6 e §8.1): a atualização de 30 s não
// apaga a página, não fecha o que está aberto nem apaga o que foi digitado, conta as mudanças e,
// com a lista diferente da que a página tem, avisa em vez de mexer. Uso: node vivo.mjs <base-url>;
// env: ORQ_PLAYWRIGHT (pasta do playwright-core), ORQ_CHROMIUM (executável).
import { pathToFileURL } from 'node:url';
import { join } from 'node:path';
import { mkdirSync } from 'node:fs';

const { chromium } = await import(pathToFileURL(join(process.env.ORQ_PLAYWRIGHT, 'index.mjs')).href);
const [base, controle] = process.argv.slice(2);
const muda = async (acao) => { const r = await fetch(`${controle}/${acao}`); if (!r.ok) falha(`controle ${acao}: ${r.status}`); };

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
await pagina.goto(base + '/', { waitUntil: 'networkidle' });

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

const ciclo = async () => {
  await Promise.all([pagina.waitForResponse((r) => r.url().includes('/vivo')), pagina.clock.runFor(30500)]);
  await pagina.waitForTimeout(300);
};
const lista = () => pagina.evaluate(() => ({
  ondas: [...document.querySelectorAll('details.onda')].map((d) => d.id).join(','),
  barraOnda3: document.querySelector('#onda-3-barra .onda-sinais')?.textContent.replace(/\s+/g, ' ').trim(),
  todos: document.querySelector('#aba-n-todos')?.textContent.trim(),
  pend: document.querySelector('#aba-n-pendencias')?.textContent.trim(),
  aviso: document.querySelector('#aviso-lista')?.textContent.trim(),
}));
const intacto = (a, b, onde) => {
  if (b.filhosDoBody !== a.filhosDoBody || b.texto !== 'texto digitado pela pessoa' || b.usAbertas.length !== 1 || b.ondasAbertas.length !== 1) falha(`${onde}: mexeu na página (body ${a.filhosDoBody}->${b.filhosDoBody}, texto ${JSON.stringify(b.texto)}, abertas ${b.ondasAbertas}/${b.usAbertas})`);
};

// A) Mudança de prioridade depois de uma resposta: a onda 3 perde a atenção 3 e desce na ordem.
//    A lista não é reordenada, o aviso aparece e a barra da onda 3, as abas e a situação seguem vivas.
{
  const a0 = await foto(), l0 = await lista();
  const st = await pagina.evaluate(async () => (await fetch('/p/faturamento/513/1/responder', { method: 'POST', headers: { 'Content-Type': 'application/x-www-form-urlencoded' }, body: 'texto=ok', redirect: 'manual' })).status);
  if (st !== 0 && st !== 303) falha(`caso A: POST = ${st}`);
  await ciclo();
  const a1 = await foto(), l1 = await lista();
  intacto(a0, a1, 'caso A');
  if (l1.ondas !== l0.ondas) falha(`caso A: a lista foi reordenada: ${l0.ondas} -> ${l1.ondas}`);
  if (!l1.aviso?.startsWith('A lista mudou')) falha(`caso A: sem o aviso (${JSON.stringify(l1.aviso)})`);
  if (l1.barraOnda3 === l0.barraOnda3) falha(`caso A: a barra da onda 3 não foi atualizada (${l1.barraOnda3})`);
  if (Number(l1.pend) !== Number(l0.pend) - 1) falha(`caso A: a aba Pendências não caiu 1 (${l0.pend} -> ${l1.pend})`);
}

// B) Onda nova: não é inserida; o aviso fica; o que a página tem segue atualizando.
{
  const a0 = await foto(), l0 = await lista();
  await muda('nova-onda');
  await ciclo();
  const a1 = await foto(), l1 = await lista();
  intacto(a0, a1, 'caso B');
  if (l1.ondas !== l0.ondas) falha(`caso B: a lista mudou sozinha: ${l0.ondas} -> ${l1.ondas}`);
  if (Number(l1.todos) !== Number(l0.todos) + 1) falha(`caso B: a aba Todos não contou a onda nova (${l0.todos} -> ${l1.todos})`);
  if (!l1.aviso?.startsWith('A lista mudou')) falha('caso B: sem o aviso');
}

// C) Onda encerrada: continua na página (nada é removido), sem erro de oob, com o aviso.
{
  const a0 = await foto(), l0 = await lista();
  await muda('encerrar-onda');
  await ciclo();
  const a1 = await foto(), l1 = await lista();
  intacto(a0, a1, 'caso C');
  if (l1.ondas !== l0.ondas) falha(`caso C: a lista mudou sozinha: ${l0.ondas} -> ${l1.ondas}`);
  if (Number(l1.todos) !== Number(l0.todos) - 1) falha(`caso C: a aba Todos não descontou a encerrada (${l0.todos} -> ${l1.todos})`);
}

// Captura do estado "lista mudou" (aviso visível, lista como estava), a 1440 e a 390 px.
if (process.env.ORQ_CAPTURAS) {
  mkdirSync(process.env.ORQ_CAPTURAS, { recursive: true });
  for (const [nome, w, h] of [['1440', 1440, 900], ['390', 390, 844]]) {
    await pagina.setViewportSize({ width: w, height: h });
    await pagina.evaluate(() => document.querySelector('#aviso-lista').scrollIntoView({ block: 'center' }));
    await pagina.screenshot({ path: join(process.env.ORQ_CAPTURAS, `lista-mudou-${nome}.png`) });
  }
  await pagina.setViewportSize({ width: 1440, height: 900 });
}

// Recarregar pelo link do aviso traz a lista nova, sem aviso.
await Promise.all([pagina.waitForNavigation({ waitUntil: 'load' }), pagina.locator('#aviso-lista a').click()]);
await pagina.waitForTimeout(500);
{
  const l = await lista();
  if (l.aviso) falha(`depois de Atualizar o aviso continua: ${l.aviso}`);
  if (l.ondas.includes('onda-8,') || l.ondas.endsWith('onda-8')) falha('onda encerrada continua na lista recarregada');
}
if (process.env.ORQ_CAPTURAS) {
  // a captura do caso "lista mudou" é feita pelo navegador.mjs; aqui só se garante o estado final
}

// Um 503 num ciclo não troca nada (hx-swap="none") e o poll continua: servidor de teste não tem
// como falhar sob demanda; o httptest cobre o status. Aqui só se confere que não houve erro de console.
for (const c of consoles) falha(c);
for (const r of respostasVivo) if (r !== 200) falha(`/vivo respondeu ${r}`);

await navegador.close();
console.log(JSON.stringify({ problemas }));
process.exit(problemas.length ? 1 : 0);
