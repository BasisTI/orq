// Porte do ContrasteDoTemaTest.java (skill basis-web-frontend). O daisyUI esmaece vários textos
// com color-mix de base-content, e abaixo de 70% isso fica sob 4,5:1 nos dois fundos do tema. O
// input.css sobrescreve cada um; este teste garante que nenhum ficou de fora, inclusive os que
// uma atualização do daisyUI venha a trazer.
//
// Roda sobre o CSS NÃO minificado (uma declaração por linha), gerado num diretório temporário:
// no minificado ele não acha regra alguma e passaria vazio. Esse CSS não é versionado nem embutido.
// Estados desabilitados e placeholders ficam de fora (a WCAG isenta componente inativo).
import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import assert from 'node:assert/strict';

const MINIMO = 70;
const COR_ESMAECIDA = /^color:\s*color-mix\(in oklab,\s*(?:var\(--color-base-content\)|currentColor)\s+(\d+)%/;
const NEGACAO = /:not\([^()]*\)/g;

function normalizar(seletor) {
  return seletor.replace(/\s+/g, ' ').replace(/\(\s+/g, '(').replace(/\s+\)/g, ')').trim();
}

function seletorDe(pilha) {
  let s = '';
  for (const nivel of pilha) {
    if (nivel.startsWith('@')) continue;
    if (nivel.startsWith('&')) s += nivel.slice(1);
    else s += (s === '' ? '' : ' ') + nivel;
  }
  return normalizar(s);
}

function condicoesDe(pilha) {
  return pilha
    .filter((n) => n.startsWith('@media') || n.startsWith('@container'))
    .map((n) => n.replace(/\s+/g, ' ') + ' | ')
    .join('');
}

// Percorre os blocos guardando a pilha. @layer e @supports ficam fora da chave (não mudam onde a
// regra vale); @media e @container entram nela.
export function cores(css) {
  const out = [];
  const pilha = [];
  css.split('\n').forEach((bruta, i) => {
    const linha = bruta.trim();
    if (linha.endsWith('{')) {
      pilha.push(linha.slice(0, -1).trim());
    } else if (linha === '}') {
      pilha.pop();
    } else {
      const m = COR_ESMAECIDA.exec(linha);
      if (m) {
        const seletor = seletorDe(pilha);
        out.push({ regra: condicoesDe(pilha) + seletor, seletor, percentual: Number(m[1]), linha: i + 1 });
      }
    }
  });
  return out;
}

// Desabilitado ou placeholder, sem contar o que está negado: :not(:disabled) é ativo.
function inativo(seletor) {
  let s = seletor;
  for (let anterior = null; s !== anterior; ) {
    anterior = s;
    s = s.replace(NEGACAO, '');
  }
  return s.includes('disabled') || s.includes('placeholder');
}

export function semSobrescrita(css) {
  const todas = cores(css);
  const falhas = [];
  for (const cor of todas) {
    if (cor.percentual >= MINIMO || inativo(cor.seletor)) continue;
    const coberta = todas.some((o) => o.linha > cor.linha && o.percentual >= MINIMO && o.regra === cor.regra);
    if (!coberta) falhas.push(`${cor.percentual}% em ${cor.regra} (linha ${cor.linha})`);
  }
  return falhas;
}

function autoteste() {
  const mix = (n) => `color: color-mix(in oklab, var(--color-base-content) ${n}%, transparent);`;
  assert.deepEqual(semSobrescrita(`.novo {\n${mix(60)}\n}\n`), ['60% em .novo (linha 2)'], 'regra nova esmaecida sem sobrescrita reprova');
  assert.equal(
    semSobrescrita(`.novo {\n&:not(:disabled) {\n${mix(60)}\n}\n}\n`).length, 1,
    ':not(:disabled) continua sendo estado ativo');
  assert.equal(
    semSobrescrita(`.novo {\n${mix(60)}\n}\n@media (min-width: 2000px) {\n.novo {\n${mix(70)}\n}\n}\n`).length, 1,
    'sobrescrita só numa largura não vale para as outras');
  assert.deepEqual(
    semSobrescrita(`@layer utilities {\n.tab {\n&:not(:checked) {\n@supports (color: color-mix(in lab, red, red)) {\n${mix(50)}\n}\n}\n}\n}\n@layer utilities {\n.tab:not(:checked) {\n${mix(70)}\n}\n}\n`), [],
    'sobrescrita posterior do mesmo seletor aprova');
  assert.deepEqual(
    semSobrescrita(`.input {\n&:is(:disabled, [disabled]) {\n${mix(40)}\n}\n}\n`), [],
    'estado desabilitado fica de fora');
}

function main() {
  autoteste();
  const raiz = fileURLToPath(new URL('.', import.meta.url));
  const tmp = mkdtempSync(join(tmpdir(), 'orq-contraste-'));
  try {
    const saida = join(tmp, 'app.css');
    // --no-install: nunca ir ao registro atrás do binário (README do script de build).
    execFileSync('npx', ['--no-install', 'tailwindcss', '-i', 'input.css', '-o', saida], { cwd: raiz, stdio: ['ignore', 'ignore', 'inherit'] });
    const css = readFileSync(saida, 'utf8');
    assert.ok(cores(css).length > 0, 'o CSS gerado deveria ter regras esmaecidas do daisyUI (o build leu o CSS linha a linha?)');
    const falhas = semSobrescrita(css);
    assert.deepEqual(falhas, [], 'texto ativo esmaecido abaixo de 70% sem sobrescrita no input.css');
    console.log(`contraste: ${cores(css).length} regras esmaecidas conferidas, nenhuma abaixo de ${MINIMO}% sem sobrescrita`);
  } finally {
    rmSync(tmp, { recursive: true, force: true });
  }
}

if (process.argv[1] === fileURLToPath(import.meta.url)) main();
