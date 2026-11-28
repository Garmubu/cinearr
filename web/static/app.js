'use strict';
/* Cinearr — interface (vanilla JS, no build step). Every value coming from
   trackers, TMDB or the user is escaped with esc() before reaching the DOM.
   Texts are written in English and translated with t() (catalogs in
   internal/i18n/locales, served by /api/i18n/<code>). */

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const IMG = (p, size = 'w342') => (p ? `https://image.tmdb.org/t/p/${size}${p}` : '');

// paint replaces the content of an element that is redrawn every few
// seconds without moving the user: nothing is touched when the content is
// the same; otherwise the scroll positions (the page and the inner .scroll
// boxes), the choices of the lists and fields with an id, the opened
// details and the content of the boxes filled afterwards (data-keep) are
// carried over. route() clears the memory of the page when it changes.
function paint(el, html) {
  if (!el || el._html === html) return false;
  const view = document.getElementById('view');
  const top = view ? view.scrollTop : 0;
  const inner = [...el.querySelectorAll('.scroll')].map((x) => [x.scrollTop, x.scrollLeft]);
  const kept = {};
  el.querySelectorAll('[data-keep]').forEach((x) => { kept[x.dataset.keep] = x.innerHTML; });
  const values = {};
  el.querySelectorAll('select[id], input[id]:not([type=checkbox]):not([type=file])').forEach((x) => { values[x.id] = x.value; });
  const open = new Set([...el.querySelectorAll('details[data-k][open]')].map((x) => x.dataset.k));
  el.innerHTML = html;
  el._html = html;
  el.querySelectorAll('[data-keep]').forEach((x) => { if (kept[x.dataset.keep] !== undefined) x.innerHTML = kept[x.dataset.keep]; });
  el.querySelectorAll('select[id], input[id]:not([type=checkbox]):not([type=file])').forEach((x) => {
    if (values[x.id] !== undefined && (x.tagName !== 'SELECT' || [...x.options].some((o) => o.value === values[x.id]))) x.value = values[x.id];
  });
  el.querySelectorAll('details[data-k]').forEach((x) => { if (open.has(x.dataset.k)) x.open = true; });
  el.querySelectorAll('.scroll').forEach((x, i) => { if (inner[i]) [x.scrollTop, x.scrollLeft] = inner[i]; });
  if (view) view.scrollTop = top;
  return true;
}

/* ------------------------------------------------------------ translation */
let LANG = 'en';
let DICT = {};
const LOCALES = { en: 'en-US', fr: 'fr-FR', es: 'es-ES', it: 'it-IT', de: 'de-DE' };
const UI_LANGUAGES = [['en', 'English'], ['fr', 'Français'], ['es', 'Español'], ['it', 'Italiano'], ['de', 'Deutsch']];

// t translates an English text; {name} placeholders are replaced by params
// (escape them first when the result goes into HTML).
function t(s, params) {
  let r = (LANG !== 'en' && DICT[s]) || s;
  if (params) r = r.replace(/\{(\w+)\}/g, (m, k) => (k in params ? String(params[k]) : m));
  return r;
}
// te is t() for HTML: the parameters are escaped.
function te(s, params) {
  const p = {};
  Object.entries(params || {}).forEach(([k, v]) => { p[k] = esc(v); });
  return t(s, p);
}

async function setLanguage(code) {
  code = UI_LANGUAGES.some(([c]) => c === code) ? code : 'en';
  if (code !== 'en') {
    try { DICT = await api('GET', '/i18n/' + code); } catch (e) { DICT = {}; }
  } else DICT = {};
  LANG = code;
  document.documentElement.lang = code;
  try { localStorage.setItem('cinearr.lang', code); } catch (e) { /* ignore */ }
}

async function api(method, path, body) {
  const r = await fetch('/api' + path, {
    method,
    headers: { 'Content-Type': 'application/json', 'X-Cinearr': '1' },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || r.statusText);
  return j;
}

/* JavaScript errors are sent to the server log (Activity › Log). */
let clientErrors = 0;
function reportClientError(kind, message, source, line, col, stack) {
  if (++clientErrors > 20) return;
  fetch('/api/client-log', {
    method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Cinearr': '1' },
    body: JSON.stringify({ kind, message: String(message || ''), source: source || '', line: line || 0, col: col || 0, stack: stack || '', page: location.hash }),
  }).catch(() => {});
}
window.addEventListener('error', (e) => reportClientError('error', e.message, e.filename, e.lineno, e.colno, e.error && e.error.stack));
window.addEventListener('unhandledrejection', (e) => {
  const r = e.reason || {};
  reportClientError('promise', r.message || String(r), '', 0, 0, r.stack);
});

const ICON = {
  discover: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>',
  library: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 4v16M17 4v16M3 9h4M3 15h4M17 9h4M17 15h4"/></svg>',
  downloads: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3v12m0 0-4-4m4 4 4-4M4 17v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2"/></svg>',
  indexers: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></svg>',
  activity: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12h4l3-8 4 16 3-8h4"/></svg>',
  settings: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/></svg>',
  play: '<svg viewBox="0 0 24 24" fill="currentColor"><path d="M7 4.5v15a1 1 0 0 0 1.5.86l12.5-7.5a1 1 0 0 0 0-1.72L8.5 3.64A1 1 0 0 0 7 4.5z"/></svg>',
  plus: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>',
  bolt: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M13 2 4 14h7l-1 8 9-12h-7z"/></svg>',
  list: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/></svg>',
  trash: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M3 6h18M8 6V4h8v2M6 6l1 14h10l1-14"/></svg>',
  folder: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/></svg>',
  sub: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="5" width="18" height="14" rx="2"/><path d="M7 15h4M14 15h3M7 11h2M12 11h5"/></svg>',
  x: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M6 6l12 12M18 6 6 18"/></svg>',
  refresh: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M20 11a8 8 0 1 0-2.3 5.7M20 4v7h-7"/></svg>',
  shield: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M12 3 4 6v6c0 5 3.5 8 8 9 4.5-1 8-4 8-9V6z"/><path d="m9 12 2 2 4-4"/></svg>',
  film: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M7 3v18M17 3v18M3 8h4M3 12h4M3 16h4M17 8h4M17 12h4M17 16h4"/></svg>',
  tv: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6"><rect x="2.5" y="6" width="19" height="13" rx="2"/><path d="M8 2.5l4 3.5 4-3.5M8 22h8"/></svg>',
  power: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3v9M6.3 6.3a8 8 0 1 0 11.4 0"/></svg>',
  chevron: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>',
  globe: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 2.7 3.8 5.7 3.8 9S14.5 18.3 12 21c-2.5-2.7-3.8-5.7-3.8-9S9.5 5.7 12 3z"/></svg>',
};

const state = { status: null, settings: null, movies: [], series: [], qualities: [], refresh: null, view: '' };

/* ---------------------------------------------------------------- helpers */
function toast(msg, kind = '') {
  const el = document.createElement('div');
  el.className = 'toast ' + kind;
  el.textContent = msg;
  $('#toasts').append(el);
  setTimeout(() => el.remove(), kind === 'bad' ? 7000 : 4000);
}
const fail = (e) => toast(e.message || String(e), 'bad');
const clientName = () => state.status?.client || t('the torrent client');

function size(n) {
  if (!n) return '—';
  const u = LANG === 'fr' ? ['o', 'Ko', 'Mo', 'Go', 'To'] : ['B', 'KB', 'MB', 'GB', 'TB'];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return (i >= 3 ? n.toLocaleString(LOCALES[LANG], { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : Math.round(n)) + ' ' + u[i];
}
function speed(n) { return n ? size(n) + '/s' : '—'; }
function eta(s) {
  if (!s || s < 0 || s >= 8640000) return '—';
  const h = Math.floor(s / 3600), m = Math.floor((s % 3600) / 60);
  return h ? `${h} h ${m} min` : `${m} min`;
}
function ago(d) {
  const ts = new Date(d).getTime();
  if (!ts || ts < 0) return '—';
  const s = (Date.now() - ts) / 1000;
  if (s < 3600) return t('{n} min', { n: Math.max(1, Math.round(s / 60)) });
  if (s < 86400) return t('{n} h', { n: Math.round(s / 3600) });
  if (s < 86400 * 60) return t('{n} d', { n: Math.round(s / 86400) });
  if (s < 86400 * 730) return t('{n} mo', { n: Math.round(s / (86400 * 30)) });
  return t('{n} y', { n: Math.round(s / (86400 * 365)) });
}
// safeUrl keeps only web links (http, https) from sites and providers.
const safeUrl = (u) => (/^https?:\/\//i.test(String(u || '').trim()) ? String(u).trim() : '');

function date(d) { const x = new Date(d); return isNaN(x) ? '' : x.toLocaleString(LOCALES[LANG], { dateStyle: 'short', timeStyle: 'short' }); }
const plural = (n, one, many) => t(n === 1 ? one : many, { n });

const STATUS = () => ({ available: t('Available'), downloading: t('Downloading'), importing: t('Importing'), wanted: t('Wanted'), failed: t('Failed') });
// download states reported by the torrent client
const DL_STATE = () => ({ paused: t('Paused'), stalled: t('Stalled'), queued: t('Queued'), metadata: t('Fetching metadata'), completed: t('Completed'), error: t('Error'), downloading: t('Downloading') });
function statusBadge(m) {
  let label = STATUS()[m.status] || m.status;
  let cls = m.status;
  if (m.status === 'downloading' && m.download) {
    const st = m.download.state;
    if (st && st !== 'downloading' && DL_STATE()[st]) { label = DL_STATE()[st]; cls = st === 'error' ? 'failed' : 'wanted'; }
    label += ' ' + Math.floor((m.download.progress || 0) * 100) + ' %';
  }
  return `<span class="badge ${esc(cls)}">${esc(label)}</span>`;
}
const KIND = () => ({ added: t('added'), grabbed: t('grabbed'), imported: t('imported'), upgraded: t('upgraded'), failed: t('failed'), blocked: t('blocked'),
  warning: t('warning'), scan: t('scan'), moved: t('folder'), refresh: t('pages'), cancelled: t('cancelled'), removed: t('removed'), deleted: t('deleted'), subtitle: t('subtitle'), played: t('played'), cleanup: t('cleanup') });
const kindTag = (k) => `<span class="tag ${k === 'imported' || k === 'subtitle' ? 'g' : k === 'failed' || k === 'blocked' ? 'r' : ''}">${esc(KIND()[k] || k)}</span>`;
function posterImg(path, title, sz) {
  return path ? `<img loading="lazy" src="${esc(IMG(path, sz))}" alt="">` : `<div class="noimg">${esc(title)}</div>`;
}
function setLoading(el, text) { el.innerHTML = `<div class="loading"><span class="spinner"></span>${esc(text || t('Loading…'))}</div>`; }

function modal({ title, body = '', footer = '', wide = false, onClose }) {
  const root = $('#modal-root');
  const wrap = document.createElement('div');
  wrap.className = 'backdrop';
  wrap.innerHTML = `<div class="modal ${wide ? 'wide' : ''}" role="dialog" aria-modal="true">
    <header><h2>${esc(title)}</h2><button class="ghost small" data-close aria-label="${esc(t('Close'))}">${ICON.x}</button></header>
    <div class="body">${body}</div>${footer ? `<footer>${footer}</footer>` : ''}</div>`;
  const close = () => { wrap.remove(); document.removeEventListener('keydown', onKey); onClose && onClose(); };
  const onKey = (e) => { if (e.key === 'Escape') close(); };
  wrap.addEventListener('mousedown', (e) => { if (e.target === wrap) close(); });
  wrap.querySelector('[data-close]').addEventListener('click', close);
  document.addEventListener('keydown', onKey);
  root.append(wrap);
  return { el: wrap.querySelector('.modal'), body: wrap.querySelector('.body'), close };
}

function confirmBox(title, html, okLabel) {
  return new Promise((resolve) => {
    const m = modal({ title, body: `<p>${html}</p>`, footer: `<button data-no>${esc(t('Cancel'))}</button><button class="primary" data-yes>${esc(okLabel || t('Confirm'))}</button>`, onClose: () => resolve(false) });
    m.el.querySelector('[data-yes]').onclick = () => { resolve(true); m.close(); };
    m.el.querySelector('[data-no]').onclick = () => { m.close(); };
  });
}

async function pick(dir, title) {
  try {
    const r = await api('POST', `/fs/pick?type=${dir ? 'dir' : 'file'}&title=${encodeURIComponent(title)}`);
    return r.path || '';
  } catch (e) { fail(e); return ''; }
}

/* ------------------------------------------------------------- navigation */
const NAV = () => [
  ['discover', t('Discover'), ICON.discover],
  ['library', t('Library'), ICON.library],
  ['downloads', t('Downloads'), ICON.downloads],
  ['indexers', t('Indexers'), ICON.indexers],
  ['activity', t('Activity'), ICON.activity],
  ['settings', t('Settings'), ICON.settings],
];

function renderNav() {
  const active = state.view;
  const dl = state.movies.filter((m) => m.download).length + state.series.reduce((a, x) => a + (x.downloads || []).length, 0);
  $('#nav').innerHTML = NAV().map(([id, label, icon]) =>
    `<a href="#/${id}" class="${active === id ? 'active' : ''}">${icon}<span>${esc(label)}</span>${id === 'downloads' && dl ? `<span class="count">${dl}</span>` : ''}</a>`).join('');
  const s = state.status;
  if (!s) return;
  const qOk = !s.syncError;
  $('#side-status').innerHTML = `
    ${s.setupDone ? `<div title="${esc(s.syncError || '')}"><span class="dot ${qOk ? 'ok' : 'bad'}"></span>${esc(qOk ? t('{name} connected', { name: clientName() }) : t('{name} unreachable', { name: clientName() }))}</div>` : ''}
    <div><span class="dot ${s.playerFound ? 'ok' : 'bad'}"></span>${esc(s.playerFound ? t('Player ready') : t('Player not found'))}</div>
    <div><span class="dot ${s.indexers ? 'ok' : 'bad'}"></span>${esc(plural(s.indexers, '{n} indexer', '{n} indexers'))}</div>
    ${(s.keyProblems || []).map((p) => `<a href="#/settings/${esc(p.tab)}" title="${esc(p.message)}"><span class="dot bad"></span>${esc(t('{name}: key to replace', { name: p.service }))}</a>`).join('')}
    <div class="row" style="justify-content:space-between;margin-top:6px"><span class="muted">v${esc(s.version)}</span>
    <button class="ghost small" data-act="quit" title="${esc(t('Quit Cinearr'))}">${ICON.power}${esc(t('Quit'))}</button></div>`;
}

async function refreshStatus() {
  try {
    const [st, movies, series] = await Promise.all([api('GET', '/status'), api('GET', '/movies'), api('GET', '/series')]);
    state.status = st;
    state.movies = movies;
    state.series = series;
    if (st.scanning) scanWasRunning = true;
    else if (scanWasRunning) { scanWasRunning = false; scanFinished(); }
    renderNav();
    checkCrash();
    checkKeyProblems();
    if (state.refresh) state.refresh();
  } catch (e) { /* server restarting */ }
}

// checkKeyProblems tells the user, once per session and service, that a
// key provided with Cinearr is refused, and leads to the settings where
// they can enter their own.
const keyProblemsShown = new Set();
function checkKeyProblems() {
  const s = state.status;
  if (!s || !s.setupDone || state.view === 'setup' || document.querySelector('#modal-root .modal')) return;
  const p = (s.keyProblems || []).find((x) => !keyProblemsShown.has(x.service));
  if (!p) return;
  keyProblemsShown.add(p.service);
  const m = modal({ title: t('{name} refuses the key provided with Cinearr', { name: p.service }),
    body: `<p>${esc(p.message)}</p><p class="muted">${esc(t('Your own key is free and only takes a few minutes to create. Cinearr uses it instead of the built-in one.'))}</p>`,
    footer: `<button data-later>${esc(t('Later'))}</button><a class="btn primary" href="#/settings/${esc(p.tab)}" data-go>${esc(t('Open the settings'))}</a>` });
  m.el.querySelector('[data-later]').onclick = () => m.close();
  m.el.querySelector('[data-go]').addEventListener('click', () => m.close());
}

// keyProblem returns the warning shown in a settings tab when the built-in
// key of one of its services is refused.
function keyProblem(tab) {
  return ((state.status && state.status.keyProblems) || []).filter((p) => p.tab === tab)
    .map((p) => `<div class="callout warn">${esc(p.message)}</div>`).join('');
}

let crashShown = false;
function checkCrash() {
  const s = state.status;
  if (!s || !s.crash || crashShown) return;
  crashShown = true;
  const m = modal({ title: t('Unexpected stop detected'), wide: true,
    body: `<p>${te('Cinearr ran into an error on {date}. The technical details were saved.', { date: date(s.crash) })}</p>
      <p class="muted">${esc(t('Export a diagnostic (log, version, settings without passwords or keys) to report it.'))}</p>
      <details><summary class="muted" style="cursor:pointer">${esc(t('Show details'))}</summary><pre class="logview" id="crash-report">${esc(t('Loading…'))}</pre></details>`,
    footer: `<button data-ack>${esc(t('Do not show again'))}</button><a class="btn primary" href="/api/diagnostic" download data-dl>${esc(t('Export a diagnostic'))}</a>` });
  api('GET', '/crash').then((r) => { const el = $('#crash-report'); if (el) el.textContent = r.report || t('(empty)'); }).catch(() => {});
  m.el.querySelector('[data-ack]').onclick = async () => { try { await api('POST', '/crash/ack'); } catch (e) { fail(e); } m.close(); };
  m.el.querySelector('[data-dl]').addEventListener('click', () => toast(t('Diagnostic saved in your Downloads folder')));
}

// ---- settings not saved
// settingsDirty compares the edited settings (with the fields of the visible
// tab) to the saved ones. Empty values ('', [], {}, null) count as absent
// and the allowed qualities as a set, so that merely showing a tab is not a
// change.
function canonical(v) {
  if (Array.isArray(v)) {
    const out = v.map(canonical);
    return out.every((x) => typeof x === 'number') ? out.sort((a, b) => a - b) : out;
  }
  if (v && typeof v === 'object') {
    const out = {};
    Object.keys(v).sort().forEach((k) => {
      const x = canonical(v[k]);
      const empty = x === '' || x == null || (Array.isArray(x) && !x.length) || (typeof x === 'object' && !Array.isArray(x) && !Object.keys(x).length);
      if (!empty) out[k] = x;
    });
    return out;
  }
  return v;
}
function settingsDirty() {
  if (state.view !== 'settings' || !edit || !state.settings || !$('#set-body')) return false;
  readTab();
  return JSON.stringify(canonical(edit)) !== JSON.stringify(canonical(state.settings));
}

let unsavedPending = ''; // where the user wanted to go
let leavingSettings = false; // the changes were saved or discarded: the next route lets go
function showUnsaved() {
  let box = $('#unsaved');
  if (!box) {
    box = document.createElement('div');
    box.id = 'unsaved';
    box.className = 'unsaved';
    box.setAttribute('role', 'alert');
    box.innerHTML = `<div class="unsaved-text">${esc(t('You have unsaved changes'))}</div>
      <div class="row"><button class="small" data-unsaved="discard">${esc(t('Discard the changes'))}</button>
      <button class="small primary" data-unsaved="save">${esc(t('Save'))}</button></div>`;
    $('#toasts').prepend(box);
    box.querySelector('[data-unsaved=save]').addEventListener('click', async (e) => {
      e.currentTarget.disabled = true;
      try { await saveSettings(true); toast(t('Settings saved'), 'ok'); leaveSettings(); } catch (err) { fail(err); e.currentTarget.disabled = false; }
    });
    box.querySelector('[data-unsaved=discard]').addEventListener('click', () => {
      edit = JSON.parse(JSON.stringify(state.settings));
      editClients(edit);
      plEdit = edit.player;
      subEdit = edit.subtitles;
      drawSettingsTab(); // the fields show the saved values again
      leaveSettings();
    });
    return;
  }
  // a new attempt: the box shakes (not with reduced motion, see app.css)
  box.classList.remove('shake');
  void box.offsetWidth;
  box.classList.add('shake');
}
function hideUnsaved() { $('#unsaved')?.remove(); }
// leaveSettings goes where the user wanted once the changes are saved or
// discarded.
function leaveSettings() {
  hideUnsaved();
  const to = unsavedPending;
  unsavedPending = '';
  if (to && to !== location.hash) {
    leavingSettings = true;
    location.hash = to;
  } else drawSettingsTab();
}

let routeHash = location.hash; // hash of the page shown, restored when leaving is refused
function route() {
  const h = location.hash.replace(/^#\/?/, '') || 'discover';
  const [view, arg] = h.split('/');
  if (state.status && !state.status.setupDone && view !== 'setup') { location.hash = '#/setup'; return; }
  // leaving the settings (or opening them again, which reloads them) with
  // changes not saved: the page stays; the tabs of the settings keep the
  // changes and do not go through here
  const leaving = leavingSettings;
  leavingSettings = false;
  if (!leaving && state.view === 'settings' && settingsDirty()) {
    unsavedPending = location.hash;
    history.replaceState(null, '', routeHash);
    showUnsaved();
    return;
  }
  hideUnsaved();
  routeHash = location.hash;
  state.view = view;
  state.refresh = null;
  historyCache.at = 0;
  renderNav();
  const v = $('#view');
  v.scrollTop = 0;
  v._html = null;
  const views = { discover: viewDiscover, library: viewLibrary, movie: viewMovie, series: viewSeries, downloads: viewDownloads, indexers: viewIndexers, settings: viewSettings, activity: viewActivity, setup: viewSetup };
  (views[view] || viewDiscover)(v, arg);
}

/* ----------------------------------------------------------------- discover */
let discoverMode = 'trending';
let discoverKind = 'movie'; // movie or tv
let lookupTimer = null;

function summaryCard(s) {
  const kind = s.kind || 'movie';
  return `<div class="poster" data-act="preview" data-id="${s.id}" data-kind="${kind}">
    <div class="img">${posterImg(s.poster_path, s.title)}
      ${s.inLibrary ? `<span class="badge lib">${esc((kind === 'movie' && STATUS()[s.status]) || t('In library'))}</span>` :
        `${s.upcoming ? `<span class="badge wanted">${esc(kind === 'tv' ? t('Not aired yet') : t('Not released yet'))}</span>` : ''}<button class="primary small add" data-act="add" data-id="${s.id}" data-kind="${kind}">${ICON.plus}${esc(t('Add'))}</button>`}
    </div>
    <div class="t" title="${esc(s.title)}">${esc(s.title)}</div>
    <div class="y">${s.year || ''}${s.vote_average ? ` <span>★ ${s.vote_average.toFixed(1)}</span>` : ''}</div>
  </div>`;
}

function viewDiscover(v) {
  v.innerHTML = `<div class="page">
    <div class="page-head">
      <h1>${esc(t('Discover'))}</h1>
      <div class="tabs seg" id="disc-kind" role="tablist">
        <button class="tab ${discoverKind === 'movie' ? 'active' : ''}" role="tab" aria-selected="${discoverKind === 'movie'}" data-act="disc-kind" data-kind="movie">${ICON.film}${esc(t('Films'))}</button>
        <button class="tab ${discoverKind === 'tv' ? 'active' : ''}" role="tab" aria-selected="${discoverKind === 'tv'}" data-act="disc-kind" data-kind="tv">${ICON.tv}${esc(t('Series'))}</button></div>
      <div class="searchbar">${ICON.discover}<input id="lookup" placeholder="${esc(discoverKind === 'tv' ? t('Search a series (title, title + year, or IMDb link / id)') : t('Search a film (title, title + year, or IMDb link / id)'))}" autocomplete="off"></div>
      <div class="chips" id="disc-mode">
        <span class="chip ${discoverMode === 'trending' ? 'active' : ''}" data-act="disc-mode" data-mode="trending">${esc(t('Trending'))}</span>
        <span class="chip ${discoverMode === 'popular' ? 'active' : ''}" data-act="disc-mode" data-mode="popular">${esc(t('Popular'))}</span>
      </div>
    </div>
    <div id="disc-title" class="muted" style="margin:-8px 0 16px"></div>
    <div id="disc"></div></div>`;
  const input = $('#lookup');
  input.focus();
  input.addEventListener('input', () => {
    clearTimeout(lookupTimer);
    lookupTimer = setTimeout(() => loadDiscover(input.value.trim()), 350);
  });
  loadDiscover('');
}

// Discover lists load page after page (20 films each) while the end of the
// grid is visible, so a large window is filled and scrolling continues.
const disc = { q: '', page: 0, loading: false, done: false, seen: new Set(), token: 0, observer: null };
const DISC_MAX_PAGES = 25;

async function loadDiscover(q) {
  const box = $('#disc');
  if (!box) return;
  disc.q = q;
  disc.page = 0;
  disc.done = false;
  disc.loading = false;
  disc.seen = new Set();
  disc.token++;
  $('#disc-title').textContent = '';
  box.innerHTML = '<div class="posters" id="disc-grid"></div><div id="disc-more" class="disc-more"></div>';
  if (disc.observer) disc.observer.disconnect();
  disc.observer = new IntersectionObserver((entries) => { if (entries.some((e) => e.isIntersecting)) loadMoreDiscover(); },
    { root: $('#view'), rootMargin: '0px 0px 900px 0px' });
  disc.observer.observe($('#disc-more'));
  await loadMoreDiscover();
}

async function loadMoreDiscover() {
  const grid = $('#disc-grid');
  const more = $('#disc-more');
  if (!grid || !more || disc.loading || disc.done) return;
  disc.loading = true;
  const token = disc.token;
  const q = disc.q;
  const pageNum = disc.page + 1;
  setLoading(more, pageNum === 1 ? t('Loading…') : t('Loading more…'));
  try {
    const r = q ? await api('GET', `/lookup?kind=${discoverKind}&q=${encodeURIComponent(q)}&page=${pageNum}`) : await api('GET', `/discover?kind=${discoverKind}&type=${discoverMode}&page=${pageNum}`);
    if (token !== disc.token || !$('#disc-grid')) return;
    const list = r.results || [];
    const fresh = list.filter((x) => !disc.seen.has(x.id));
    fresh.forEach((x) => disc.seen.add(x.id));
    disc.page = pageNum;
    grid.insertAdjacentHTML('beforeend', fresh.map(summaryCard).join(''));
    if (!r.more || pageNum >= DISC_MAX_PAGES || (!fresh.length && q)) disc.done = true;
    const n = disc.seen.size;
    $('#disc-title').textContent = q ? t(n === 1 ? '{n} result for “{q}”' : '{n} results for “{q}”', { n: n + (disc.done ? '' : '+'), q }) :
      (discoverKind === 'tv' ? (discoverMode === 'trending' ? t('The series everyone is talking about this week') : t('The most popular series right now')) :
        (discoverMode === 'trending' ? t('The films everyone is talking about this week') : t('The most popular films right now')));
    more.innerHTML = '';
    if (!n && disc.done) {
      $('#disc').innerHTML = `<div class="empty">${ICON.film}<h2>${esc(discoverKind === 'tv' ? t('No series found') : t('No film found'))}</h2><p>${esc(t('Try the original title, or add the year.'))}</p></div>`;
      return;
    }
  } catch (e) {
    if (token !== disc.token) return;
    if (pageNum === 1) {
      $('#disc').innerHTML = `<div class="empty">${ICON.film}<h2>${esc(t('Could not query TMDB'))}</h2><p>${esc(e.message)}</p><p><a href="#/settings/general">${esc(t('Check the TMDB key'))}</a></p></div>`;
    } else {
      more.innerHTML = `<div class="muted">${esc(e.message)} <button class="small" data-act="disc-more">${esc(t('Retry'))}</button></div>`;
    }
    disc.done = pageNum === 1;
    return;
  } finally {
    if (token === disc.token) disc.loading = false;
  }
  // the window is not full yet: keep loading
  requestAnimationFrame(() => {
    const m = $('#disc-more');
    const view = $('#view');
    if (m && view && !disc.done && m.getBoundingClientRect().top < view.getBoundingClientRect().bottom + 900) loadMoreDiscover();
  });
}

// addMovie adds the film to the library; the search starts only when the
// user asks for it (or when "search on add" is enabled in the settings).
async function addMovie(id, el) {
  if (el) el.disabled = true;
  try {
    const m = await api('POST', '/movies', { tmdbId: +id, search: true });
    toast(state.settings?.searchOnAdd ? t('{title} added, looking for the best release…', { title: m.title }) : t('{title} added to the library', { title: m.title }), 'ok');
    markAdded('movie', m.tmdbId);
    await refreshStatus();
  } catch (e) { fail(e); if (el) el.disabled = false; }
}

// markAdded shows on the spot that a film or a series is in the library:
// the user stays on the page to add other titles (a click on the poster
// then opens its page).
function markAdded(kind, id) {
  document.querySelectorAll(`.poster[data-kind="${kind}"][data-id="${id}"] .add`).forEach((b) => {
    b.outerHTML = `<span class="badge lib">${esc(t('In library'))}</span>`;
  });
}

async function previewMovie(id) {
  const inLib = state.movies.find((m) => m.tmdbId === +id);
  if (inLib) { location.hash = '#/movie/' + id; return; }
  const m = modal({ title: t('Film'), body: `<div class="loading"><span class="spinner"></span>${esc(t('Loading…'))}</div>`, wide: true });
  try {
    const d = await api('GET', '/tmdb/' + id);
    const director = (d.credits?.crew || []).filter((c) => c.job === 'Director').map((c) => c.name).join(', ');
    const cast = (d.credits?.cast || []).slice(0, 8).map((c) => c.name).join(', ');
    const upcoming = !d.release_date || new Date(d.release_date) > new Date();
    m.el.querySelector('h2').textContent = `${d.title} (${d.year || '?'})`;
    m.body.innerHTML = `<div class="preview">
      <div class="preview-poster">${posterImg(d.poster_path, d.title, 'w500')}</div>
      <div class="grow">
        <div class="muted" style="margin-bottom:10px">${esc([d.year, d.runtime ? d.runtime + ' min' : '', (d.genres || []).map((g) => g.name).join(', ')].filter(Boolean).join(' · '))}${d.vote_average ? ` · ★ ${d.vote_average.toFixed(1)}` : ''}</div>
        ${upcoming ? `<div class="callout warn">${esc(t('This film is not released yet: no release can be found for now.'))}</div>` : ''}
        ${d.original_title && d.original_title !== d.title ? `<p class="muted">${esc(t('Original title:'))} <b>${esc(d.original_title)}</b></p>` : ''}
        <p>${esc(d.overview || t('No overview.'))}</p>
        ${director ? `<p class="muted">${esc(t('Directed by:'))} <b style="color:var(--text)">${esc(director)}</b></p>` : ''}
        ${cast ? `<p class="muted">${esc(t('Starring:'))} ${esc(cast)}</p>` : ''}
        ${d.imdb_id ? `<p class="muted">IMDb : <span class="mono">${esc(d.imdb_id)}</span></p>` : ''}
      </div></div>`;
    const foot = document.createElement('footer');
    foot.innerHTML = `<button data-close2>${esc(t('Close'))}</button><button class="primary" data-addnow>${ICON.plus}${esc(t('Add to the library'))}</button>`;
    m.el.append(foot);
    foot.querySelector('[data-close2]').onclick = m.close;
    foot.querySelector('[data-addnow]').onclick = async (e) => { await addMovie(id, e.currentTarget); m.close(); };
  } catch (e) { m.body.innerHTML = `<p class="bad-text">${esc(e.message)}</p>`; }
}

/* ------------------------------------------------------------------ library */
let libFilter = 'all';
let libText = '';
let libKind = 'films'; // films or series

function movieCard(m) {
  const p = m.download ? Math.floor((m.download.progress || 0) * 100) : 0;
  return `<a class="poster" href="#/movie/${m.tmdbId}" style="color:inherit;text-decoration:none">
    <div class="img">${posterImg(m.posterPath, m.title)}${statusBadge(m)}
    ${m.download ? `<div class="bar"><i style="width:${p}%"></i></div>` : ''}</div>
    <div class="t" title="${esc(m.title)}">${esc(m.title)}</div>
    <div class="y">${m.year || ''}${m.file ? ` · ${esc(m.file.qualityName)}` : ''}</div></a>`;
}

function viewLibrary(v, arg) {
  if (arg === 'series' || arg === 'films') libKind = arg;
  const filters = libKind === 'series' ? [['all', t('All')], ['active', t('In progress')], ['missing', t('Missing episodes')], ['complete', t('Complete')]] :
    [['all', t('All')], ['available', t('Available')], ['active', t('In progress')], ['wanted', t('Wanted')], ['failed', t('Failed')]];
  if (!filters.some(([k]) => k === libFilter)) libFilter = 'all';
  v.innerHTML = `<div class="page">
    <div class="page-head"><h1>${esc(t('Library'))}</h1><span class="sub" id="lib-count"></span><div class="spacer"></div>
      <button data-act="scan" id="scan-btn" title="${esc(t('Reads the films and series folders again: files added, moved or deleted by hand are taken into account'))}">${ICON.refresh}${esc(t('Update the library'))}</button>
      <div class="searchbar" style="max-width:300px">${ICON.discover}<input id="lib-text" placeholder="${esc(t('Filter'))}" value="${esc(libText)}"></div></div>
    <div id="meta-progress"></div><div id="scan-report"></div>
    <div class="tabs" role="tablist">
      <button class="tab ${libKind === 'films' ? 'active' : ''}" role="tab" aria-selected="${libKind === 'films'}" data-act="lib-kind" data-k="films">${ICON.film}${esc(t('Films'))} <span class="muted" id="lib-n-films">${state.movies.length}</span></button>
      <button class="tab ${libKind === 'series' ? 'active' : ''}" role="tab" aria-selected="${libKind === 'series'}" data-act="lib-kind" data-k="series">${ICON.tv}${esc(t('Series'))} <span class="muted" id="lib-n-series">${state.series.length}</span></button></div>
    <div class="chips" style="margin-bottom:20px">
      ${filters.map(([k, l]) => `<span class="chip ${libFilter === k ? 'active' : ''}" data-act="lib-filter" data-f="${k}">${esc(l)}</span>`).join('')}
    </div><div id="lib"></div></div>`;
  $('#lib-text').addEventListener('input', (e) => { libText = e.target.value; drawLibrary(); });
  if (!lastScan) api('GET', '/library/scan').then((r) => { lastScan = r; drawScan(); }).catch(() => {});
  state.refresh = drawLibrary;
  drawLibrary();
}

function drawLibrary() {
  const box = $('#lib');
  if (!box) return;
  drawScan();
  // the counts of the tabs follow the lists (films added by a scan…)
  $('#lib-n-films').textContent = state.movies.length;
  $('#lib-n-series').textContent = state.series.length;
  if (libKind === 'series') { drawSeriesLibrary(box); return; }
  let list = state.movies;
  if (libFilter === 'available') list = list.filter((m) => m.status === 'available');
  if (libFilter === 'active') list = list.filter((m) => m.status === 'downloading' || m.status === 'importing');
  if (libFilter === 'wanted') list = list.filter((m) => m.status === 'wanted');
  if (libFilter === 'failed') list = list.filter((m) => m.status === 'failed');
  if (libText) { const x = libText.toLowerCase(); list = list.filter((m) => (m.title + ' ' + (m.originalTitle || '')).toLowerCase().includes(x)); }
  $('#lib-count').textContent = plural(state.movies.length, '{n} film', '{n} films');
  paint(box, list.length ? `<div class="posters">${list.map(movieCard).join('')}</div>` :
    `<div class="empty">${ICON.library}<h2>${esc(state.movies.length ? t('No film in this filter') : t('Your library is empty'))}</h2>
     <p>${state.movies.length ? '' : t('Search a film in <a href="#/discover">Discover</a> and add it, then start a search from its page.')}</p></div>`);
}

function drawSeriesLibrary(box) {
  let list = state.series;
  if (libFilter === 'active') list = list.filter((s) => (s.downloads || []).length || s.downloading);
  if (libFilter === 'missing') list = list.filter((s) => s.available < s.episodes);
  if (libFilter === 'complete') list = list.filter((s) => s.episodes && s.available >= s.episodes);
  if (libText) { const x = libText.toLowerCase(); list = list.filter((s) => (s.title + ' ' + (s.originalTitle || '')).toLowerCase().includes(x)); }
  $('#lib-count').textContent = plural(state.series.length, '{n} show', '{n} shows');
  paint(box, list.length ? `<div class="posters">${list.map(seriesCard).join('')}</div>` :
    `<div class="empty">${ICON.tv}<h2>${esc(state.series.length ? t('No series in this filter') : t('No series yet'))}</h2>
     <p>${state.series.length ? '' : t('Search a series in <a href="#/discover">Discover</a> and add it, then choose the seasons or episodes to download from its page.')}</p></div>`);
}


/* -------------------------------------------------------------- folder scan */
// The films and series folders are read at each launch and with the
// "Update the library" button (files added, moved or deleted by hand).
let lastScan = null;
let scanWasRunning = false;

async function startScan() {
  try {
    await api('POST', '/library/scan');
    scanWasRunning = true;
    toast(t('Reading the films and series folders…'));
    refreshStatus();
  } catch (e) { fail(e); }
}

// scanFinished is called by refreshStatus when a scan ends.
async function scanFinished() {
  try { lastScan = await api('GET', '/library/scan'); } catch (e) { return; }
  const added = (lastScan.added || []).length;
  toast(t('Library updated: {added} added, {found} files found, {missing} missing', { added, found: lastScan.linked, missing: (lastScan.missing || []).length }), 'ok');
  if (state.view === 'library') drawScan();
}

function drawScan() {
  const btn = $('#scan-btn');
  const running = !!state.status?.scanning;
  const meta = state.status?.metadata;
  const mbox = $('#meta-progress');
  if (mbox) mbox.innerHTML = meta?.running ? `<div class="callout"><span class="spinner"></span> ${esc(t('Reloading the film and series pages in the new language: {done} of {total}', { done: meta.done, total: meta.total }))}</div>` : '';
  if (btn) {
    btn.disabled = running;
    btn.innerHTML = running ? `<span class="spinner"></span>${esc(t('Reading the folders…'))}` : `${ICON.refresh}${esc(t('Update the library'))}`;
  }
  const box = $('#scan-report');
  if (!box) return;
  const r = lastScan;
  if (!r || running || !r.finished || (!(r.unknown || []).length && !(r.missing || []).length && !(r.errors || []).length && !(r.notes || []).length)) { box.innerHTML = ''; box.dataset.at = ''; return; }
  // the page is redrawn every few seconds: the same report is left as it
  // is, so a detail the user opened stays open
  if (box.dataset.at === r.finished && box.innerHTML) return;
  box.dataset.at = r.finished;
  const list = (items) => `<ul class="mono scan-list">${items.slice(0, 50).map((x) => `<li>${esc(x)}</li>`).join('')}${items.length > 50 ? `<li>…</li>` : ''}</ul>`;
  box.innerHTML = `${(r.notes || []).map((n) => `<div class="callout warn">${esc(n)}</div>`).join('')}<div class="callout">${esc(t('Last update of the library: {date}', { date: date(r.finished) }))}
    ${(r.unknown || []).length ? `<details><summary>${esc(plural(r.unknown.length, '{n} video not recognised (name it “Title (Year)” to help)', '{n} videos not recognised (name them “Title (Year)” to help)'))}</summary>${list(r.unknown)}</details>` : ''}
    ${(r.missing || []).length ? `<details><summary>${esc(plural(r.missing.length, '{n} file no longer found', '{n} files no longer found'))}</summary>${list(r.missing)}</details>` : ''}
    ${(r.errors || []).length ? `<details><summary>${esc(t('Errors'))}</summary>${list(r.errors)}</details>` : ''}</div>`;
}

/* -------------------------------------------------------------------- movie */
function viewMovie(v, id) {
  id = +id;
  let extra = null;
  const draw = () => {
    const m = state.movies.find((x) => x.tmdbId === id);
    if (!m) { paint(v, `<div class="page"><div class="empty"><h2>${esc(t('Film not found'))}</h2><p><a href="#/library">${esc(t('Back to the library'))}</a></p></div></div>`); return; }
    const busy = state.status?.busy?.[id];
    const d = m.download;
    const p = d ? Math.floor((d.progress || 0) * 100) : 0;
    const director = extra ? (extra.credits?.crew || []).filter((c) => c.job === 'Director').map((c) => c.name).join(', ') : '';
    const cast = extra ? (extra.credits?.cast || []).slice(0, 10) : [];
    const trailer = extra ? (extra.videos?.results || []).find((x) => x.site === 'YouTube' && x.type === 'Trailer') : null;
    const sideDownload = d ? `<div class="card"><h2>${esc(t('Downloading'))}</h2>
        <div class="row" style="justify-content:space-between;margin-bottom:8px"><b>${p} %</b><span class="muted">${esc(DL_STATE()[d.state] || d.state || '')}</span></div>
        <div class="progress"><i style="width:${p}%"></i></div>
        <dl class="kv" style="margin-top:14px"><dt>${esc(t('Speed'))}</dt><dd>${speed(d.dlSpeed)}</dd><dt>${esc(t('Remaining'))}</dt><dd>${eta(d.eta)}</dd>
          <dt>${esc(t('Seeds'))}</dt><dd>${d.seeds ?? '—'}</dd><dt>${esc(t('Size'))}</dt><dd>${size(d.size)}</dd><dt>${esc(t('Quality'))}</dt><dd>${esc(d.qualityName)}</dd>
          <dt>${esc(t('Indexer'))}</dt><dd>${esc(d.indexer)}</dd><dt>${esc(t('Checked'))}</dt><dd>${d.checked ? `<span class="ok-text">${esc(t('safe content'))}</span>` : `<span class="warn-text">${esc(t('waiting for metadata'))}</span>`}</dd></dl>
        <div class="rel-title muted" style="margin-top:12px">${esc(d.releaseTitle)}</div>
        <div class="row" style="margin-top:14px;flex-wrap:wrap">
          ${p > 3 && d.checked ? `<button data-act="play-live">${ICON.play}${esc(t('Play while downloading'))}</button>` : ''}
          <button class="danger" data-act="cancel">${ICON.x}${esc(t('Cancel'))}</button></div></div>` : '';
    const sideFile = m.file ? `<div class="card"><h2>${esc(t('File'))}</h2>
        <dl class="kv"><dt>${esc(t('Quality'))}</dt><dd><span class="tag q">${esc(m.file.qualityName)}</span></dd><dt>${esc(t('Size'))}</dt><dd>${size(m.file.size)}</dd>
        ${m.file.releaseGroup ? `<dt>${esc(t('Group'))}</dt><dd>${esc(m.file.releaseGroup)}</dd>` : ''}
        ${(m.file.languages || []).length ? `<dt>${esc(t('Languages'))}</dt><dd>${m.file.languages.map((l) => `<span class="tag l">${esc(l)}</span>`).join('')}</dd>` : ''}
        <dt>${esc(t('Imported'))}</dt><dd>${date(m.file.imported)}</dd><dt>${esc(t('Path'))}</dt><dd class="mono">${esc(m.file.path)}</dd></dl>
        <div class="row" style="margin-top:14px;flex-wrap:wrap"><button data-act="open-folder">${ICON.folder}${esc(t('Open the folder'))}</button></div></div>
      <div class="card"><h2>${esc(t('Subtitles'))}</h2>
        ${(m.subtitles || []).length ? `<div class="list">${m.subtitles.map((s) => `<div class="row"><span class="tag l">${esc(s.language.toUpperCase())}</span>
          ${s.hi ? `<span class="tag">${esc(t('HI'))}</span>` : ''}${s.forced ? `<span class="tag">${esc(t('forced'))}</span>` : ''}<span class="muted grow">${esc(s.provider)} · ${s.score} %</span></div>`).join('')}</div>` :
          `<p class="muted">${esc(t('No subtitles yet.'))}</p>`}
        <div style="margin-top:12px"><button data-act="subs">${ICON.sub}${esc(t('Search subtitles'))}</button></div></div>` : '';
    paint(v, `
      <div class="hero"><div class="backdrop" style="background-image:url('${esc(IMG(m.backdropPath, 'w1280'))}')"></div>
        <div class="inner"><div class="pimg">${posterImg(m.posterPath, m.title, 'w500')}</div>
          <div style="flex:1;min-width:0">
            ${statusBadge(m)}
            <h1 style="margin-top:10px">${esc(m.title)} <span class="muted" style="font-weight:400">(${m.year || '?'})</span></h1>
            <div class="meta">${m.runtime ? `<span>${m.runtime} min</span>` : ''}${(m.genres || []).length ? `<span>${esc(m.genres.join(', '))}</span>` : ''}
              ${m.rating ? `<span>★ <b>${m.rating.toFixed(1)}</b></span>` : ''}${m.imdbId ? `<span class="mono">${esc(m.imdbId)}</span>` : ''}
              ${m.originalTitle && m.originalTitle !== m.title ? `<span>${esc(t('Original title:'))} <b>${esc(m.originalTitle)}</b></span>` : ''}</div>
            <div class="overview">${esc(m.overview || '')}</div>
            ${m.statusMessage ? `<div class="callout warn" style="max-width:760px">${esc(m.statusMessage)}</div>` : ''}
            <div class="actions">
              ${m.file ? `<button class="primary big" data-act="play">${ICON.play}${esc(t('Play'))}</button>${subtitlePick('play-sub', m.subtitles)}` : ''}
              <button class="${m.file || d ? '' : 'primary'}" data-act="autosearch" ${busy ? 'disabled' : ''}>${busy ? `<span class="spinner"></span>${esc(t('Searching…'))}` : ICON.bolt + esc(m.file ? t('Search a better release') : t('Automatic search'))}</button>
              <button data-act="manual">${ICON.list}${esc(t('Manual search'))}</button>
              ${trailer ? `<a class="btn" href="https://www.youtube.com/watch?v=${esc(trailer.key)}" target="_blank" rel="noopener">${ICON.play}${esc(t('Trailer'))}</a>` : ''}
              <button class="ghost" data-act="refresh-movie" title="${esc(t('Refresh the information'))}">${ICON.refresh}</button>
              <button class="ghost danger" data-act="delete-movie" title="${esc(t('Delete'))}">${ICON.trash}</button>
            </div></div></div></div>
      <div class="movie-body">
        <div>
          ${extra && (cast.length || director) ? `<div class="card credits"><h2>${esc(t('Cast and crew'))}</h2>${creditsHTML(extra.credits, 20)}</div>` : ''}
          <div class="card"><h2>${esc(t('History'))}</h2><div id="movie-history" class="muted" data-keep="movie-history-${id}">${esc(t('Loading…'))}</div></div>
        </div>
        <div>${sideDownload}${sideFile}${!d && !m.file ? `<div class="card"><h2>${esc(t('Not downloaded yet'))}</h2><p class="muted">${te('Automatic search queries your indexers, discards suspicious releases and sends the best one to {client}. Manual search shows every result so you can choose.', { client: clientName() })}</p>
          <div class="row" style="flex-wrap:wrap"><button class="primary" data-act="autosearch" ${busy ? 'disabled' : ''}>${ICON.bolt}${esc(t('Automatic search'))}</button><button data-act="manual">${ICON.list}${esc(t('Manual search'))}</button></div></div>` : ''}</div>
      </div>`);
    loadHistory('movie-history', (e) => e.tmdbId === id);
  };
  state.refresh = draw;
  draw();
  api('GET', '/tmdb/' + id).then((x) => { extra = x; if (state.view === 'movie') draw(); }).catch(() => {});
}

// subtitlePick is the list of the subtitles of a film or an episode, to
// choose the one active at the launch (or none). It is only shown with the
// players that honour the choice (VLC, mpv, MPC-HC) and when there is a
// subtitle file; it starts on the first file in the preferred languages.
const SUB_PLAYERS = ['vlc', 'mpv', 'mpc-hc'];
function subtitlePick(id, subs) {
  if (!(subs || []).length || !SUB_PLAYERS.includes(state.status?.playerFamily)) return '';
  const prefs = (state.settings?.subtitles?.languages || []).map((l) => l.toLowerCase());
  let def = 0;
  for (const l of prefs) {
    const i = subs.findIndex((x) => (x.language || '').toLowerCase() === l);
    if (i >= 0) { def = i; break; }
  }
  const label = (x) => [String(x.language || '').toUpperCase(), x.hi ? t('HI') : '', x.forced ? t('forced') : '',
    x.provider === 'manual' ? t('added from a file') : x.provider].filter(Boolean).join(' · ');
  return `<select id="${esc(id)}" class="sub-pick" title="${esc(t('Subtitles at the launch'))}" aria-label="${esc(t('Subtitles at the launch'))}">
    ${subs.map((x, i) => `<option value="${i}" ${i === def ? 'selected' : ''}>${esc(label(x))}</option>`).join('')}
    <option value="none">${esc(t('No subtitles'))}</option></select>`;
}

// subQuery is the choice of a subtitle list for the play request
// ("sub=…", or "" when the list is not shown).
function subQuery(id) {
  const el = document.getElementById(id);
  return el ? 'sub=' + encodeURIComponent(el.value) : '';
}

// loadHistory shows the last events of a film or a series. The pages are
// redrawn every few seconds: the history is fetched at most every 10 s.
let historyCache = { at: 0, list: [] };
async function loadHistory(boxId, keep) {
  try {
    if (Date.now() - historyCache.at > 10000) historyCache = { at: Date.now(), list: await api('GET', '/history') };
    const h = historyCache.list.filter(keep).slice(0, 15);
    const box = document.getElementById(boxId);
    if (!box) return;
    box.innerHTML = h.length ? h.map((e) => `<div style="padding:5px 0;border-bottom:1px solid #1f2430">${kindTag(e.kind)}
      <span style="color:var(--text)">${esc(e.message)}</span> <span class="muted">· ${date(e.date)}</span></div>`).join('') : esc(t('No event.'));
  } catch (e) { /* ignore */ }
}

let lastDecisions = [];
async function manualSearch(id) {
  const m = modal({ title: t('Manual search'), body: `<div class="loading"><span class="spinner"></span>${esc(t('Querying the indexers… (up to a minute)'))}</div>`, wide: true });
  try {
    const r = await api('POST', `/movies/${id}/search`);
    m.body.innerHTML = decisionTable(r, `data-act="grab" data-movie="${id}"`, false);
  } catch (e) { m.body.innerHTML = `<p class="bad-text">${esc(e.message)}</p>`; }
}

let lastSubs = [];
async function subtitleSearch(id) { return subtitleModal(`/movies/${id}/subtitles`, t('Subtitles')); }

// subtitleExtras offers the sites added by the user and the import of a
// file, under the results of a subtitle search.
function subtitleExtras(path, links) {
  return `<div class="sub-extra">
    ${links.length ? `<div class="row wrap"><span class="muted">${esc(t('Your sites:'))}</span>${links.filter((l) => safeUrl(l.url)).map((l) => `<a class="btn small" href="${esc(safeUrl(l.url))}" target="_blank" rel="noopener">${ICON.globe}${esc(t('Search on {site}', { site: l.name }))}</a>`).join('')}</div>` : ''}
    <div class="row wrap"><span class="muted">${esc(t('Found it elsewhere?'))}</span><button class="small" data-act="sub-import" data-path="${esc(path)}">${ICON.plus}${esc(t('Add a subtitle file'))}</button></div>
  </div>`;
}

async function subtitleModal(path, title) {
  const m = modal({ title, body: `<div class="loading"><span class="spinner"></span>${esc(t('Searching subtitles…'))}</div>`, wide: true });
  try {
    const r = await api('GET', path);
    lastSubs = r.candidates || [];
    const links = r.links || [];
    m.body.innerHTML = `${(r.errors || []).map((e) => `<div class="callout warn">${esc(e)}</div>`).join('')}
      ${lastSubs.length ? `<div class="scroll"><table><thead><tr><th>${esc(t('Language'))}</th><th>Release</th><th>Score</th><th>${esc(t('Matches'))}</th><th>${esc(t('Source'))}</th><th></th></tr></thead><tbody>
      ${lastSubs.map((c, i) => `<tr><td><span class="tag l">${esc((c.language || '?').toUpperCase())}</span>${c.hi ? `<span class="tag">${esc(t('HI'))}</span>` : ''}${c.forced ? `<span class="tag">${esc(t('forced'))}</span>` : ''}</td>
        <td class="rel-title">${esc(c.release)}</td><td><b class="${c.percent >= 80 ? 'ok-text' : c.percent >= 50 ? 'warn-text' : 'bad-text'}">${c.percent} %</b></td>
        <td>${c.hashMatch ? '<span class="tag g">hash</span>' : ''}${(c.matches || []).filter((x) => x !== 'hash').map((x) => `<span class="tag">${esc(x)}</span>`).join('')}</td>
        <td>${esc(c.provider)}<div class="muted" style="font-size:12px">${c.downloads ? esc(plural(c.downloads, '{n} download', '{n} downloads')) : ''}</div></td>
        <td><button class="small primary" data-act="sub-dl" data-i="${i}" data-path="${esc(path)}" aria-label="${esc(t('Download'))}">${ICON.downloads}</button></td></tr>`).join('')}</tbody></table></div>` :
        `<div class="empty"><h2>${esc(t('No subtitles found'))}</h2><p>${t('Check the providers in <a href="#/settings/subtitles">Settings › Subtitles</a>.')}</p></div>`}
      ${subtitleExtras(path, links)}`;
  } catch (e) { m.body.innerHTML = `<p class="bad-text">${esc(e.message)}</p><p><a href="#/settings/subtitles">${esc(t('Settings › Subtitles'))}</a></p>${subtitleExtras(path, [])}`; }
}

/* ---------------------------------------------------------------- downloads */
function viewDownloads(v) {
  v.innerHTML = `<div class="page"><div class="page-head"><h1>${esc(t('Downloads'))}</h1><span class="sub" id="dl-sync"></span></div><div id="dl"></div></div>`;
  const draw = () => {
    const box = $('#dl');
    if (!box) return;
    const items = [];
    state.movies.filter((m) => m.download || m.status === 'importing').forEach((m) => items.push({
      href: `#/movie/${m.tmdbId}`, poster: m.posterPath, name: `${m.title} (${m.year})`, d: m.download || {}, importing: m.status === 'importing',
      cancel: `data-act="cancel" data-movie="${m.tmdbId}"`, added: m.download?.added || '' }));
    state.series.forEach((s) => (s.downloads || []).forEach((d) => items.push({
      href: `#/series/${s.tmdbId}`, poster: s.posterPath, name: `${s.title} · ${d.label}`, d, importing: !!d.importing,
      cancel: `data-act="sdl-cancel" data-series="${s.tmdbId}" data-hash="${esc(d.hash)}"`, added: d.added || '' })));
    items.sort((a, b) => String(b.added).localeCompare(String(a.added)));
    const st0 = state.status;
    $('#dl-sync').textContent = st0?.syncError || '';
    paint(box, items.length ? `<div class="list">${items.map((x) => {
      const d = x.d;
      const p = Math.floor((d.progress || 0) * 100);
      const st = d.state && d.state !== 'downloading' ? DL_STATE()[d.state] : '';
      return `<div class="item"><a class="thumb" href="${x.href}">${x.poster ? `<img src="${esc(IMG(x.poster, 'w92'))}" alt="">` : ''}</a>
        <div class="grow"><div class="row"><a class="name" href="${x.href}" style="color:var(--text)">${esc(x.name)}</a>
          <span class="tag q">${esc(d.qualityName || '')}</span>${d.checked ? `<span class="tag g" title="${esc(t('File list checked'))}">${ICON.shield.replace('<svg', '<svg width="11" height="11"')} ${esc(t('safe'))}</span>` : ''}
          ${st ? `<span class="tag ${d.state === 'error' ? 'r' : ''}">${esc(st)}</span>` : ''}<span class="spacer"></span>
          <span class="muted">${x.importing ? esc(t('Importing…')) : `${p} % · ${speed(d.dlSpeed)} · ${eta(d.eta)} · ${esc(plural(d.seeds ?? 0, '{n} seed', '{n} seeds'))}`}</span></div>
          <div class="progress" style="margin:8px 0 6px"><i style="width:${p}%"></i></div>
          <div class="det rel-title">${esc(d.releaseTitle || '')}</div></div>
        <button class="ghost danger" ${x.cancel} title="${esc(t('Cancel'))}" aria-label="${esc(t('Cancel'))}">${ICON.x}</button></div>`;
    }).join('')}</div>` : `<div class="empty">${ICON.downloads}<h2>${esc(t('No download in progress'))}</h2><p>${te('Films and episodes appear here while {client} downloads them.', { client: clientName() })}</p></div>`);
  };
  state.refresh = draw;
  draw();
}

// cancelDownload asks whether to blocklist the release too (off by default).
function cancelDownload(id) {
  const m = state.movies.find((x) => x.tmdbId === +id);
  cancelModal(m?.download?.releaseTitle || '', `/movies/${id}/cancel`);
}

/* ------------------------------------------------------------------- series */
const pad2 = (n) => String(n).padStart(2, '0');
const epKey = (s, e) => `${s}:${e}`;
const epLabel = (s, e) => `S${pad2(s)}E${pad2(e)}`;
const today = () => new Date().toISOString().slice(0, 10);
const aired = (e) => !!e.airDate && e.airDate.slice(0, 10) <= today();
function dayDate(d) { const x = new Date(d); return isNaN(x) ? '' : x.toLocaleDateString(LOCALES[LANG], { dateStyle: 'medium' }); }
const TV_STATE = () => ({ 'Returning Series': t('Returning series'), Ended: t('Ended'), Canceled: t('Cancelled'), 'In Production': t('In production'), Planned: t('Planned'), Pilot: t('Pilot') });
const seasonName = (n) => (n === 0 ? t('Specials') : t('Season {n}', { n }));

const seriesCache = {}; // id -> full series
let seriesSel = new Set(); // selected episodes "season:episode"
let seriesSelId = 0;
const seriesSeason = {}; // id -> season shown
let seriesView = null; // { id, draw, load }

function seriesCard(s) {
  const dls = s.downloads || [];
  let badge;
  if (dls.length) badge = `<span class="badge downloading">${esc(t('Downloading'))}${dls.length > 1 ? ' · ' + dls.length : ''}</span>`;
  else if (s.episodes && s.available >= s.episodes) badge = `<span class="badge available">${esc(t('Complete'))}</span>`;
  else if (s.available) badge = `<span class="badge wanted">${esc(t('{n} / {total} episodes', { n: s.available, total: s.episodes }))}</span>`;
  else if (s.failed) badge = `<span class="badge failed">${esc(t('Failed'))}</span>`;
  else badge = ''; // nothing downloaded yet: the series page shows the episodes
  const p = dls.length ? Math.floor((dls.reduce((a, d) => a + (d.progress || 0), 0) / dls.length) * 100) : 0;
  return `<a class="poster" href="#/series/${s.tmdbId}" style="color:inherit;text-decoration:none">
    <div class="img">${posterImg(s.posterPath, s.title)}${badge}${dls.length ? `<div class="bar"><i style="width:${p}%"></i></div>` : ''}</div>
    <div class="t" title="${esc(s.title)}">${esc(s.title)}</div>
    <div class="y">${s.year || ''}${s.seasons ? ` · ${esc(plural(s.seasons, '{n} season', '{n} seasons'))}` : ''}</div></a>`;
}

async function addSeries(id, el) {
  if (el) el.disabled = true;
  try {
    const s = await api('POST', '/series', { tmdbId: +id });
    toast(t('{title} added to the library', { title: s.title }), 'ok');
    markAdded('tv', s.tmdbId);
    await refreshStatus();
  } catch (e) { fail(e); if (el) el.disabled = false; }
}

async function previewSeries(id) {
  if (state.series.find((s) => s.tmdbId === +id)) { location.hash = '#/series/' + id; return; }
  const m = modal({ title: t('TV series'), body: `<div class="loading"><span class="spinner"></span>${esc(t('Loading…'))}</div>`, wide: true });
  try {
    const d = await api('GET', '/tmdb/tv/' + id);
    const creators = (d.created_by || []).map((c) => c.name).join(', ');
    const cast = (d.credits?.cast || []).slice(0, 8).map((c) => c.name).join(', ');
    const network = (d.networks || []).map((n) => n.name).join(', ');
    m.el.querySelector('h2').textContent = `${d.name} (${d.year || '?'})`;
    const facts = [d.year, d.number_of_seasons ? plural(d.number_of_seasons, '{n} season', '{n} seasons') : '',
      d.number_of_episodes ? plural(d.number_of_episodes, '{n} episode', '{n} episodes') : '', network, (d.genres || []).map((g) => g.name).join(', ')].filter(Boolean);
    m.body.innerHTML = `<div class="preview">
      <div class="preview-poster">${posterImg(d.poster_path, d.name, 'w500')}</div>
      <div class="grow">
        <div class="muted" style="margin-bottom:10px">${esc(facts.join(' · '))}${d.vote_average ? ` · ★ ${d.vote_average.toFixed(1)}` : ''}</div>
        ${d.original_name && d.original_name !== d.name ? `<p class="muted">${esc(t('Original title:'))} <b>${esc(d.original_name)}</b></p>` : ''}
        <p>${esc(d.overview || t('No overview.'))}</p>
        ${creators ? `<p class="muted">${esc(t('Created by:'))} <b style="color:var(--text)">${esc(creators)}</b></p>` : ''}
        ${cast ? `<p class="muted">${esc(t('Starring:'))} ${esc(cast)}</p>` : ''}
        <p class="muted">${esc(t('Adding the series downloads nothing: you then choose the seasons or episodes to search.'))}</p>
      </div></div>`;
    const foot = document.createElement('footer');
    foot.innerHTML = `<button data-close2>${esc(t('Close'))}</button><button class="primary" data-addnow>${ICON.plus}${esc(t('Add to the library'))}</button>`;
    m.el.append(foot);
    foot.querySelector('[data-close2]').onclick = m.close;
    foot.querySelector('[data-addnow]').onclick = async (e) => { await addSeries(id, e.currentTarget); m.close(); };
  } catch (e) { m.body.innerHTML = `<p class="bad-text">${esc(e.message)}</p>`; }
}

function epStatus(e, d, can) {
  if (d) {
    const p = Math.floor((d.progress || 0) * 100);
    if (d.importing || e.status === 'importing') return `<span class="badge importing">${esc(t('Importing'))}</span>`;
    const st = d.state && d.state !== 'downloading' && DL_STATE()[d.state] ? DL_STATE()[d.state] : t('Downloading');
    const cls = d.state === 'error' ? 'failed' : d.state && d.state !== 'downloading' ? 'wanted' : 'downloading';
    return `<span class="badge ${cls}">${esc(st)} ${p} %</span>`;
  }
  if (e.file) return `<span class="badge available">${esc(t('Available'))}</span> <span class="tag q">${esc(e.file.qualityName)}</span>${(e.subtitles || []).map((s) => `<span class="tag l">${esc(s.language.toUpperCase())}</span>`).join('')}`;
  if (e.status === 'failed') return `<span class="badge failed">${esc(t('Failed'))}</span>`;
  if (e.status === 'wanted') return `<span class="badge wanted">${esc(t('Wanted'))}</span>`;
  if (!can) return `<span class="muted">${esc(e.airDate ? t('Not aired yet') : t('Date unknown'))}</span>`;
  return `<span class="muted">${esc(t('Not downloaded'))}</span>`;
}

function viewSeries(v, id) {
  id = +id;
  if (seriesSelId !== id) { seriesSel = new Set(); seriesSelId = id; }
  let data = seriesCache[id] || null;
  const load = async () => {
    try {
      const x = await api('GET', '/series/' + id);
      data = x;
      seriesCache[id] = x;
      if (state.view === 'series' && movieId() === id) draw();
    } catch (e) {
      if (!data) v.innerHTML = `<div class="page"><div class="empty"><h2>${esc(t('Series not found'))}</h2><p><a href="#/library/series">${esc(t('Back to the library'))}</a></p></div></div>`;
    }
  };
  const draw = () => {
    if (!data) return;
    const s = data;
    const busy = state.status?.busySeries?.[id];
    const seasons = [...(s.seasons || [])].sort((a, b) => ((a.number === 0) - (b.number === 0)) || a.number - b.number);
    let cur = seriesSeason[id];
    if (cur === undefined || !seasons.some((x) => x.number === cur)) {
      const firstMissing = seasons.find((x) => x.number > 0 && x.episodes.some((e) => !e.file && aired(e)));
      cur = (firstMissing || seasons.find((x) => x.number > 0) || seasons[0] || { number: 0 }).number;
      seriesSeason[id] = cur;
    }
    const se = seasons.find((x) => x.number === cur);
    const dlBy = {};
    (s.downloads || []).forEach((d) => { dlBy[d.hash.toLowerCase()] = d; });
    const eps = se ? se.episodes : [];
    const selectable = eps.filter((e) => aired(e) || e.file);
    const allOn = selectable.length > 0 && selectable.every((e) => seriesSel.has(epKey(e.season, e.number)));
    const nSel = seriesSel.size;
    const anyFile = seasons.some((x) => x.episodes.some((e) => e.file));
    const rows = eps.map((e) => {
      const k = epKey(e.season, e.number);
      const can = aired(e) || !!e.file;
      const d = e.downloadHash ? dlBy[e.downloadHash.toLowerCase()] : null;
      const attrs = `data-season="${e.season}" data-episode="${e.number}"`;
      const acts = [];
      if (e.file) {
        acts.push(`<button class="primary play-btn" data-act="ep-play" ${attrs}>${ICON.play}${esc(t('Play'))}</button>`);
        acts.push(`<button data-act="ep-subs" ${attrs} title="${esc(t('Search subtitles'))}">${ICON.sub}${esc(t('Subtitles'))}</button>`);
      } else if (d && d.checked && (d.progress || 0) > 0.03) {
        acts.push(`<button class="play-btn" data-act="ep-play-live" ${attrs} title="${esc(t('Play while downloading'))}">${ICON.play}${esc(t('Play'))}</button>`);
      }
      if (can) acts.push(`<button class="ghost" data-act="ep-manual" ${attrs} title="${esc(t('Manual search'))}" aria-label="${esc(t('Manual search'))}">${ICON.list}</button>`);
      const open = episodeOpen.has(k);
      acts.push(`<button class="ghost unfold ${open ? 'open' : ''}" data-act="ep-open" data-k="${k}" aria-expanded="${open}" title="${esc(open ? t('Hide the details') : t('Details, cast and subtitles'))}" aria-label="${esc(t('Details, cast and subtitles'))}">${ICON.chevron}</button>`);
      const msg = !e.file && !d && e.statusMessage ? `<div class="why">${esc(e.statusMessage)}</div>` : '';
      const panel = open ? `<tr class="ep-panel"><td colspan="6">${episodePanel(s, e)}</td></tr>` : '';
      return `<tr class="${seriesSel.has(k) ? 'sel' : ''}${can ? '' : ' future'}${open ? ' open' : ''}">
        <td class="ep-check"><input type="checkbox" data-act="ep-check" data-k="${k}" ${seriesSel.has(k) ? 'checked' : ''} ${can ? '' : 'disabled'} aria-label="${esc(epLabel(e.season, e.number))}"></td>
        <td class="mono ep-num">${pad2(e.number)}</td>
        <td><div class="ep-title">${esc(e.title || t('Episode {n}', { n: e.number }))}</div>${e.overview ? `<div class="muted ep-ov" title="${esc(e.overview)}">${esc(e.overview)}</div>` : ''}${msg}</td>
        <td class="muted nowrap">${e.airDate ? esc(dayDate(e.airDate)) : '—'}</td>
        <td class="ep-st">${epStatus(e, d, can)}</td>
        <td class="nowrap right ep-acts">${acts.join(' ')}</td></tr>${panel}`;
    }).join('');
    const dls = (s.downloads || []).map((d) => {
      const p = Math.floor((d.progress || 0) * 100);
      const st = d.importing ? t('Importing…') : `${p} % · ${speed(d.dlSpeed)} · ${eta(d.eta)}`;
      return `<div class="item"><div class="grow"><div class="row"><b>${esc(d.label)}</b><span class="tag q">${esc(d.qualityName || '')}</span>
        ${d.state && d.state !== 'downloading' && DL_STATE()[d.state] ? `<span class="tag ${d.state === 'error' ? 'r' : ''}">${esc(DL_STATE()[d.state])}</span>` : ''}<span class="spacer"></span><span class="muted">${esc(st)}</span></div>
        <div class="progress" style="margin:8px 0 6px"><i style="width:${p}%"></i></div><div class="det rel-title">${esc(d.releaseTitle)}</div></div>
        <button class="ghost danger" data-act="sdl-cancel" data-series="${id}" data-hash="${esc(d.hash)}" title="${esc(t('Cancel'))}" aria-label="${esc(t('Cancel'))}">${ICON.x}</button></div>`;
    }).join('');
    const facts = [s.year, s.network, (s.genres || []).join(', '), s.runtime ? t('{n} min per episode', { n: s.runtime }) : '', s.state ? (TV_STATE()[s.state] || s.state) : ''].filter(Boolean);
    paint(v, `
      <div class="hero"><div class="backdrop" style="background-image:url('${esc(IMG(s.backdropPath, 'w1280'))}')"></div>
        <div class="inner"><div class="pimg">${posterImg(s.posterPath, s.title, 'w500')}</div>
          <div style="flex:1;min-width:0">
            <span class="badge lib">${esc(t('TV series'))}</span>
            <h1 style="margin-top:10px">${esc(s.title)} <span class="muted" style="font-weight:400">(${s.year || '?'})</span></h1>
            <div class="meta">${facts.map((f) => `<span>${esc(f)}</span>`).join('')}${s.rating ? `<span>★ <b>${s.rating.toFixed(1)}</b></span>` : ''}${s.imdbId ? `<span class="mono">${esc(s.imdbId)}</span>` : ''}
              ${s.originalTitle && s.originalTitle !== s.title ? `<span>${esc(t('Original title:'))} <b>${esc(s.originalTitle)}</b></span>` : ''}</div>
            <div class="overview">${esc(s.overview || '')}</div>
            <div class="actions">
              ${anyFile ? `<button data-act="series-folder">${ICON.folder}${esc(t('Open the folder'))}</button>` : ''}
              <button class="ghost" data-act="series-refresh" title="${esc(t('Refresh the information'))}" aria-label="${esc(t('Refresh the information'))}">${ICON.refresh}</button>
              <button class="ghost danger" data-act="series-delete" title="${esc(t('Delete'))}" aria-label="${esc(t('Delete'))}">${ICON.trash}</button>
            </div></div></div></div>
      <div class="page" style="padding-top:8px">
        <div class="tabs" role="tablist">${seasons.map((x) => {
          const have = x.episodes.filter((e) => e.file).length;
          return `<button class="tab ${x.number === cur ? 'active' : ''}" role="tab" aria-selected="${x.number === cur}" data-act="season" data-n="${x.number}">${esc(seasonName(x.number))} <span class="muted">${have}/${x.episodes.length}</span></button>`;
        }).join('')}</div>
        <div class="ep-toolbar">
          <label class="check"><input type="checkbox" data-act="ep-all" ${allOn ? 'checked' : ''} ${selectable.length ? '' : 'disabled'}>${esc(t('Whole season'))}</label>
          <span class="muted">${esc(nSel ? plural(nSel, '{n} episode selected', '{n} episodes selected') : t('Tick the episodes to download'))}</span>
          ${nSel ? `<button class="ghost small" data-act="ep-none">${esc(t('Clear'))}</button>` : ''}
          <span class="spacer"></span>
          ${busy ? `<span class="muted"><span class="spinner"></span> ${esc(t('Searching…'))}</span>` : ''}
          <button class="primary" data-act="ep-auto" ${nSel && !busy ? '' : 'disabled'}>${ICON.bolt}${esc(t('Automatic search'))}</button>
          <button data-act="ep-manual-sel" ${nSel === 1 ? '' : 'disabled'} title="${esc(t('Select a single episode'))}">${ICON.list}${esc(t('Manual search'))}</button>
          ${cur > 0 ? `<button class="ghost" data-act="season-manual" title="${esc(t('Manual search of the whole season, season packs included'))}">${ICON.list}${esc(t('Season packs'))}</button>` : ''}
        </div>
        ${eps.length ? `<div class="card" style="padding:4px 0"><div class="scroll"><table class="eps"><thead><tr><th></th><th>#</th><th>${esc(t('Title'))}</th><th>${esc(t('Aired'))}</th><th>${esc(t('Status'))}</th><th></th></tr></thead><tbody>${rows}</tbody></table></div></div>` :
          `<div class="empty"><h2>${esc(t('No episode listed for this season yet'))}</h2></div>`}
        <div class="movie-body" style="padding:20px 0 0">
          <div class="card"><h2>${esc(t('History'))}</h2><div id="series-history" class="muted" data-keep="series-history-${id}">${esc(t('Loading…'))}</div></div>
          <div>${dls ? `<div class="card"><h2>${esc(t('Downloads'))}</h2><div class="list">${dls}</div></div>` : ''}</div>
        </div>
      </div>`);
    loadHistory('series-history', (e) => e.seriesId === id);
  };
  seriesView = { id, draw, load };
  state.refresh = load;
  if (data) draw(); else setLoading(v);
  load();
}

function selectedEpisodes() {
  return [...seriesSel].map((k) => { const [s, e] = k.split(':').map(Number); return { season: s, episode: e }; });
}

// decisionTable renders the results of a manual search (films and series).
function decisionTable(r, grabAttrs, series) {
  // releases of another film or series are not shown: the sites often
  // return unrelated results for titles they do not know
  const all = r.decisions || [];
  lastDecisions = all.filter((d) => d.titleMatched !== false);
  const hidden = all.length - lastDecisions.length;
  const idx = (r.indexers || []).map((i) => `<span class="tag ${i.error ? 'r' : 'g'}" title="${esc(i.error || '')}">${esc(i.name)} : ${i.error ? esc(t('error')) : i.count}</span>`).join('');
  const ok = lastDecisions.filter((d) => d.approved).length;
  return `<div class="row" style="flex-wrap:wrap;margin-bottom:12px">${idx}<span class="spacer"></span>
      <span class="muted">${esc(plural(lastDecisions.length, '{n} result', '{n} results'))} · <b class="ok-text">${esc(plural(ok, '{n} accepted', '{n} accepted'))}</b>${hidden ? ` · ${esc(plural(hidden, '{n} result for another title left out', '{n} results for other titles left out'))}` : ''}</span></div>
      ${lastDecisions.length ? `<div class="scroll"><table><thead><tr><th>Release</th>${series ? `<th>${esc(t('Episodes'))}</th>` : ''}<th>${esc(t('Quality'))}</th><th>${esc(t('Size'))}</th><th>${esc(t('Seeds'))}</th><th>${esc(t('Age'))}</th><th>${esc(t('Indexer'))}</th><th title="${esc(t('Bonus of your preferences: +20 for your first language (5 less for each next one), the preferred terms of the profile (MULTi +10, VFF +5, HDR +2 by default) and +1 for freeleech. Quality, seeds and size are compared apart.'))}">${esc(t('Bonus'))}</th><th></th></tr></thead><tbody>
      ${lastDecisions.map((d, i) => `<tr class="${d.approved ? '' : 'rejected'}">
        <td><div class="rel-title">${safeUrl(d.release.infoUrl) ? `<a href="${esc(safeUrl(d.release.infoUrl))}" target="_blank" rel="noopener">${esc(d.release.title)}</a>` : esc(d.release.title)}</div>
          <div>${(d.languages || []).map((l) => `<span class="tag l">${esc(l)}</span>`).join('')}${(d.parsed?.releaseGroup || d.episode?.releaseGroup) ? `<span class="tag">${esc(d.parsed?.releaseGroup || d.episode?.releaseGroup)}</span>` : ''}${d.release.downloadVolumeFactor === 0 ? '<span class="tag g">freeleech</span>' : ''}</div>
          ${(d.rejections || []).map((x) => `<div class="why">✕ ${esc(x)}</div>`).join('')}${(d.warnings || []).map((x) => `<div class="warn">⚠ ${esc(x)}</div>`).join('')}</td>
        ${series ? `<td class="nowrap">${d.label ? `<span class="tag">${esc(d.label === 'Complete' ? t('Complete series') : d.label)}</span>` : ''}</td>` : ''}
        <td><span class="tag q">${esc(d.quality.name)}</span></td><td>${size(d.release.size)}</td>
        <td>${d.release.seeders}</td><td>${ago(d.release.publishDate)}</td><td>${esc(d.release.indexer)}</td><td>${d.score}</td>
        <td><button class="${d.approved ? 'primary' : ''} small nowrap" ${grabAttrs} data-i="${i}" title="${esc(d.approved ? '' : t('Download despite the reasons above'))}">${ICON.downloads}${esc(d.approved ? t('Download') : t('Download anyway'))}</button></td></tr>`).join('')}
      </tbody></table></div>` : `<div class="empty"><h2>${esc(t('No release found'))}</h2><p>${esc(hidden ? (series ? t('Your sites only returned releases of other titles: this series is probably not on them.') : t('Your sites only returned releases of other titles: this film is probably not on them.')) :
        (series ? t('Check your indexers, or the series title on the sites.') : t('Check your indexers, or the film title on the sites.')))}</p></div>`}`;
}

let lastSeriesKeys = [];
async function seriesManualSearch(id, season, episode) {
  const title = episode ? t('Manual search · {label}', { label: epLabel(season, episode) }) : t('Manual search · {season}', { season: seasonName(season) });
  const m = modal({ title, body: `<div class="loading"><span class="spinner"></span>${esc(t('Querying the indexers… (up to a minute)'))}</div>`, wide: true });
  const s = seriesCache[id];
  if (episode) lastSeriesKeys = [{ season, episode }];
  else lastSeriesKeys = ((s?.seasons || []).find((x) => x.number === season)?.episodes || []).filter((e) => aired(e) || e.file).map((e) => ({ season, episode: e.number }));
  try {
    const r = await api('POST', `/series/${id}/search?season=${season}&episode=${episode || 0}`);
    m.body.innerHTML = decisionTable(r, `data-act="ep-grab" data-series="${id}"`, true);
  } catch (e) { m.body.innerHTML = `<p class="bad-text">${esc(e.message)}</p>`; }
}

// cancelModal asks whether to blocklist the release too (off by default).
function cancelModal(releaseTitle, path) {
  const md = modal({ title: t('Cancel the download'),
    body: `<p>${te('The torrent and its files will be removed from {client}.', { client: clientName() })}</p>
      ${releaseTitle ? `<p class="rel-title muted">${esc(releaseTitle)}</p>` : ''}
      <label class="check"><input type="checkbox" id="cancel-block">${esc(t('Also blocklist this release (it will never be downloaded again)'))}</label>`,
    footer: `<button data-no>${esc(t('Back'))}</button><button class="primary" data-yes>${esc(t('Cancel the download'))}</button>` });
  md.el.querySelector('[data-no]').onclick = md.close;
  md.el.querySelector('[data-yes]').onclick = async () => {
    const block = $('#cancel-block')?.checked ? 1 : 0;
    try { await api('POST', `${path}${path.includes('?') ? '&' : '?'}blocklist=${block}`); md.close(); toast(t('Download cancelled')); await refreshStatus(); }
    catch (e) { fail(e); }
  };
}


/* ------------------------------------------------------- people and panels */
// peopleRow shows actors or crew members with their photo (initials when
// TMDB has none).
function peopleRow(list) {
  return `<div class="people">${list.map((p) => {
    const initials = (p.name || '?').split(/\s+/).map((w) => w[0]).slice(0, 2).join('').toUpperCase();
    return `<div class="person"><div class="ph">${p.profile_path ? `<img loading="lazy" src="${esc(IMG(p.profile_path, 'w185'))}" alt="">` : `<span>${esc(initials)}</span>`}</div>
      <b title="${esc(p.name)}">${esc(p.name)}</b><span class="muted" title="${esc(p.role || '')}">${esc(p.role || '')}</span></div>`;
  }).join('')}</div>`;
}

// CREW_JOBS are the crew members shown, in this order.
const CREW_JOBS = ['Director', 'Screenplay', 'Writer', 'Novel', 'Story', 'Teleplay', 'Creator', 'Director of Photography', 'Original Music Composer', 'Producer'];
const JOB = () => ({ Director: t('Director'), Screenplay: t('Screenplay'), Writer: t('Writer'), Novel: t('Novel'), Story: t('Story'), Teleplay: t('Teleplay'),
  Creator: t('Creator'), 'Director of Photography': t('Cinematography'), 'Original Music Composer': t('Music'), Producer: t('Producer') });

// creditsHTML returns the cast and crew rows of TMDB credits.
function creditsHTML(credits, maxCast) {
  if (!credits) return '';
  const seen = new Set();
  const cast = [...(credits.cast || []), ...(credits.guest_stars || [])].filter((p) => !seen.has(p.id) && seen.add(p.id))
    .slice(0, maxCast).map((p) => ({ ...p, role: p.character }));
  const crew = [];
  const byPerson = new Map();
  (credits.crew || []).filter((p) => CREW_JOBS.includes(p.job)).sort((a, b) => CREW_JOBS.indexOf(a.job) - CREW_JOBS.indexOf(b.job)).forEach((p) => {
    const job = JOB()[p.job] || p.job;
    if (byPerson.has(p.id)) { const x = byPerson.get(p.id); if (!x.role.includes(job)) x.role += ', ' + job; return; }
    const x = { ...p, role: job };
    byPerson.set(p.id, x);
    crew.push(x);
  });
  return `${cast.length ? `<h3>${esc(t('Cast'))}</h3>${peopleRow(cast)}` : ''}${crew.length ? `<h3>${esc(t('Crew'))}</h3>${peopleRow(crew.slice(0, 12))}` : ''}`;
}

const episodeOpen = new Set(); // unfolded episodes "season:episode"
const episodeInfo = {}; // "series:season:episode" -> { credits, links }

// episodePanel is the unfolded part of an episode: overview, playback,
// subtitles (search, sites, file) and cast.
function episodePanel(s, e) {
  const key = `${s.tmdbId}:${e.season}:${e.number}`;
  const info = episodeInfo[key];
  if (!info) {
    episodeInfo[key] = { loading: true };
    api('GET', `/series/${s.tmdbId}/episode?season=${e.season}&episode=${e.number}`)
      .then((x) => { episodeInfo[key] = x; seriesView?.draw(); })
      .catch((err) => { episodeInfo[key] = { creditsError: err.message }; seriesView?.draw(); });
  }
  const attrs = `data-season="${e.season}" data-episode="${e.number}"`;
  const subPath = `/series/${s.tmdbId}/subtitles?season=${e.season}&episode=${e.number}`;
  const subs = e.subtitles || [];
  return `<div class="ep-detail">
    <div class="ep-main">
      ${e.overview ? `<p class="ep-full">${esc(e.overview)}</p>` : ''}
      ${e.file ? `<div class="row wrap"><button class="primary big" data-act="ep-play" ${attrs}>${ICON.play}${esc(t('Play'))}</button>${subtitlePick(`ep-sub-${e.season}-${e.number}`, subs)}
        <span class="tag q">${esc(e.file.qualityName)}</span><span class="muted">${size(e.file.size)}</span></div>
        <div class="rel-title muted" style="margin-top:6px">${esc(e.file.releaseTitle || '')}</div>` : `<p class="muted">${esc(t('Not downloaded yet: tick it and start a search, or use the manual search.'))}</p>`}
      <h3>${esc(t('Subtitles'))}</h3>
      ${subs.length ? `<div class="list">${subs.map((x) => `<div class="row"><span class="tag l">${esc(x.language.toUpperCase())}</span>${x.hi ? `<span class="tag">${esc(t('HI'))}</span>` : ''}
        <span class="muted grow">${esc(x.provider === 'manual' ? t('added from a file') : x.provider)}${x.score ? ` · ${x.score} %` : ''}</span></div>`).join('')}</div>` :
        `<p class="muted">${esc(e.file ? t('No subtitles yet.') : t('Subtitles can be added once the episode is downloaded.'))}</p>`}
      ${e.file ? `<div class="row wrap" style="margin-top:10px"><button data-act="ep-subs" ${attrs}>${ICON.sub}${esc(t('Search subtitles'))}</button>
        <button data-act="sub-import" data-path="${esc(subPath)}">${ICON.plus}${esc(t('Add a subtitle file'))}</button>
        ${(info?.links || []).filter((l) => safeUrl(l.url)).map((l) => `<a class="btn" href="${esc(safeUrl(l.url))}" target="_blank" rel="noopener">${ICON.globe}${esc(t('Search on {site}', { site: l.name }))}</a>`).join('')}</div>` : ''}
    </div>
    <div class="ep-people">${!info || info.loading ? `<div class="loading"><span class="spinner"></span>${esc(t('Loading the cast…'))}</div>` :
      info.credits ? (creditsHTML(info.credits, 12) || `<p class="muted">${esc(t('No cast listed for this episode.'))}</p>`) : `<p class="muted">${esc(info.creditsError || '')}</p>`}</div>
  </div>`;
}

const SUB_LANGS = ['en', 'fr', 'es', 'it', 'de', 'pt', 'pt-br', 'nl', 'ar', 'ru', 'pl', 'tr', 'ja', 'ko', 'zh'];

// importSubtitleModal adds a subtitle file downloaded from any site to a
// film or an episode (path: its subtitle search path).
function importSubtitleModal(path) {
  const mine = (state.settings?.subtitles?.languages || []).map((x) => x.toLowerCase());
  const langs = [...new Set([...mine, ...SUB_LANGS])];
  const m = modal({ title: t('Add a subtitle file'),
    body: `<p class="muted" style="margin-top:0">${esc(t('A subtitle downloaded from any site (.srt, .ass, .ssa, .vtt, .sub, or a .zip containing one): it is saved next to the video and the player loads it.'))}</p>
      <div class="field"><label for="imp-lang">${esc(t('Language'))}</label><select id="imp-lang">${langs.map((l) => `<option value="${esc(l)}">${esc(l.toUpperCase())}</option>`).join('')}</select></div>
      <div class="field"><label for="imp-file">${esc(t('File'))}</label><input type="file" id="imp-file" accept=".srt,.ass,.ssa,.vtt,.sub,.zip"></div>
      <span id="imp-res"></span>`,
    footer: `<button data-no>${esc(t('Cancel'))}</button><button class="primary" data-yes>${esc(t('Add'))}</button>` });
  m.el.querySelector('[data-no]').onclick = m.close;
  m.el.querySelector('[data-yes]').onclick = async (ev) => {
    const file = $('#imp-file', m.el).files[0];
    const res = $('#imp-res', m.el);
    if (!file) { res.innerHTML = `<span class="bad-text">${esc(t('Choose a file'))}</span>`; return; }
    const btn = ev.currentTarget;
    btn.disabled = true;
    const [base, query] = path.split('?');
    const q = new URLSearchParams(query || '');
    q.set('lang', $('#imp-lang', m.el).value);
    q.set('name', file.name);
    try {
      const r = await fetch('/api' + base + '/import?' + q.toString(), { method: 'POST', headers: { 'Content-Type': 'application/octet-stream', 'X-Cinearr': '1' }, body: await file.arrayBuffer() });
      const j = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(j.error || r.statusText);
      m.close();
      $('#modal-root').innerHTML = '';
      toast(t('Subtitles added'), 'ok');
      await refreshStatus();
      seriesView?.load();
    } catch (err) { res.innerHTML = `<span class="bad-text">${esc(err.message)}</span>`; btn.disabled = false; }
  };
}

// needSeriesFolder asks for the series folder the first time episodes are
// downloaded, when it was not chosen in the setup.
function needSeriesFolder() {
  if (!state.status || state.status.seriesPath) return Promise.resolve(true);
  return new Promise((resolve) => {
    const m = modal({ title: t('Where to store the series?'),
      body: `<p class="muted" style="margin-top:0">${esc(t('Choose the folder where the episodes will be stored (each series in “Title (Year)/Season 01”). You can change it later in Settings › Library.'))}</p>
        <div class="field"><label for="sf-path">${esc(t('Series folder'))}</label><div class="row"><input id="sf-path" class="grow" autocomplete="off">
        <button data-act="pick" data-target="sf-path" aria-label="${esc(t('Browse'))}">${ICON.folder}</button></div></div><span id="sf-res"></span>`,
      footer: `<button data-no>${esc(t('Cancel'))}</button><button class="primary" data-yes>${esc(t('Save and continue'))}</button>`,
      onClose: () => resolve(false) });
    m.el.querySelector('[data-no]').onclick = m.close;
    m.el.querySelector('[data-yes]').onclick = async () => {
      const path = $('#sf-path', m.el).value.trim();
      if (!path) { $('#sf-res', m.el).innerHTML = `<span class="bad-text">${esc(t('Choose a folder'))}</span>`; return; }
      try {
        const cur = await api('GET', '/settings');
        cur.seriesPath = path;
        state.settings = await api('PUT', '/settings', cur);
        await refreshStatus();
        resolve(true);
        m.close();
      } catch (err) { $('#sf-res', m.el).innerHTML = `<span class="bad-text">${esc(err.message)}</span>`; }
    };
  });
}

// deleteModal removes a film or a series from the library, with its files
// when the box is ticked.
function deleteModal(title, name, filesLabel, path, done, back, after) {
  const days = state.settings?.recycleDays || 0;
  const binNote = filesLabel ? `<div class="hint muted" style="margin:-4px 0 0 30px;font-size:12.5px">${esc(days > 0
    ? plural(days, 'The files go to the recycle bin of the library folder for {n} day.', 'The files go to the recycle bin of the library folder for {n} days.')
    : t('The files are deleted for good (recycle bin disabled in Settings › Library).'))}</div>` : '';
  const md = modal({ title, body: `<p>${te('Remove {title} from the library?', { title: name || '' })}</p>${filesLabel ? `<label class="check"><input type="checkbox" id="del-files">${esc(filesLabel)}</label>${binNote}` : ''}`,
    footer: `<button data-no>${esc(t('Cancel'))}</button><button class="primary" data-yes>${esc(t('Delete'))}</button>` });
  md.el.querySelector('[data-no]').onclick = md.close;
  md.el.querySelector('[data-yes]').onclick = async () => {
    const files = $('#del-files')?.checked ? 1 : 0;
    try { await api('DELETE', `${path}?files=${files}`); md.close(); toast(done); if (after) after(); await refreshStatus(); location.hash = back; } catch (e) { fail(e); }
  };
}

function resetModal() {
  const md = modal({ title: t('Reset Cinearr'),
    body: `<p>${esc(t('Cinearr returns to its first launch and the setup wizard starts again.'))}</p>
      <p class="muted">${esc(t('Erased: the settings (TMDB key, torrent client, folders, indexers, subtitle providers, player, language), the lists of films and series of the library, the history and the blocklist.'))}</p>
      <p class="ok-text">${esc(t('Kept: every file on disk. The films and series of the folders you choose in the wizard are added to the library again.'))}</p>
      <label class="check"><input type="checkbox" id="reset-players">${esc(t('Also delete the players downloaded by Cinearr'))}</label>`,
    footer: `<button data-no>${esc(t('Cancel'))}</button><button class="danger" data-yes>${esc(t('Reset'))}</button>` });
  md.el.querySelector('[data-no]').onclick = md.close;
  md.el.querySelector('[data-yes]').onclick = async (e) => {
    e.currentTarget.disabled = true;
    try {
      await api('POST', '/reset', { players: !!$('#reset-players')?.checked });
      try { localStorage.removeItem('cinearr.setupStep'); localStorage.removeItem('cinearr.lang'); } catch (x) { /* ignore */ }
      location.hash = '#/setup';
      location.reload();
    } catch (x) { fail(x); e.currentTarget.disabled = false; }
  };
}

/* ----------------------------------------------------------------- indexers */
function viewIndexers(v) {
  v.innerHTML = `<div class="page"><div class="page-head"><h1>${esc(t('Indexers'))}</h1><span class="sub">${esc(t('The sites where films are searched (Prowlarr’s Cardigann engine)'))}</span><div class="spacer"></div>
    <button class="primary" data-act="add-indexer">${ICON.plus}${esc(t('Add an indexer'))}</button></div><div id="idx"></div></div>`;
  drawIndexers();
}

// pauseBadge shows a source paused after failures in a row (automatic
// searches leave it alone until then), with a button to resume it now.
function pauseBadge(st, kind, id) {
  if (!st || !st.pausedUntil || new Date(st.pausedUntil) <= new Date()) return '';
  const until = new Date(st.pausedUntil).toLocaleTimeString(LOCALES[LANG], { hour: '2-digit', minute: '2-digit' });
  return `<span class="tag warn" title="${esc(st.lastError || '')}">${esc(t('Paused until {time} ({n} failures in a row)', { time: until, n: st.failures }))}</span>
    <button class="small ghost" data-act="resume-source" data-kind="${kind}" data-id="${esc(id)}">${esc(t('Resume'))}</button>`;
}

async function sourceStatus() {
  try { return await api('GET', '/sources/status'); } catch (e) { return {}; }
}

async function drawIndexers() {
  const box = $('#idx');
  if (!box) return;
  state.settings = await api('GET', '/settings');
  const list = state.settings.indexers || [];
  const paused = (await sourceStatus()).indexers || {};
  box.innerHTML = list.length ? `<div class="list">${list.map((c) => `<div class="item">
      <input type="checkbox" data-act="idx-toggle" data-id="${esc(c.id)}" ${c.enabled ? 'checked' : ''} title="${esc(t('Enabled'))}">
      <div class="grow"><div class="name">${esc(c.name)}</div><div class="det">${esc(c.kind === 'torznab' ? 'Torznab · ' + c.baseUrl : (c.definitionId + (c.baseUrl ? ' · ' + c.baseUrl : '')))} · ${esc(t('priority {n}', { n: c.priority }))}${c.noProxy && state.settings.proxy?.type ? ' · ' + esc(t('without proxy')) : ''}</div></div>
      ${pauseBadge(paused[c.id], 'indexers', c.id)}
      <span id="test-${esc(c.id)}" class="muted"></span>
      <button class="small" data-act="idx-test" data-id="${esc(c.id)}">${esc(t('Test'))}</button>
      <button class="small" data-act="idx-edit" data-id="${esc(c.id)}">${esc(t('Edit'))}</button>
      <button class="small ghost danger" data-act="idx-del" data-id="${esc(c.id)}" aria-label="${esc(t('Delete'))}">${ICON.trash}</button></div>`).join('')}</div>` :
    `<div class="empty">${ICON.indexers}<h2>${esc(t('No indexer'))}</h2><p>${esc(t('Add at least one site: the list comes from the Prowlarr project and updates itself.'))}</p>
     <p><button class="primary" data-act="add-indexer">${ICON.plus}${esc(t('Add an indexer'))}</button></p></div>`;
}

let defsCache = null;
async function addIndexerModal() {
  const m = modal({ title: t('Add an indexer'), wide: true, body: `<div class="loading"><span class="spinner"></span>${esc(t('Fetching the list of sites…'))}</div>` });
  try {
    if (!defsCache) defsCache = await api('GET', '/indexers/definitions');
    const defs = defsCache.definitions || [];
    m.body.innerHTML = `${defsCache.warning ? `<div class="callout warn">${esc(defsCache.warning)}</div>` : ''}
      <div class="row" style="margin-bottom:12px"><input id="def-q" placeholder="${esc(t('Search a site ({n} available)', { n: defs.length }))}">
        <select id="def-type" style="max-width:170px"><option value="">${esc(t('All types'))}</option><option value="public">${esc(t('Public'))}</option><option value="semi-private">${esc(t('Semi-private'))}</option><option value="private">${esc(t('Private'))}</option></select>
        <select id="def-lang" style="max-width:150px"><option value="">${esc(t('All languages'))}</option><option value="en">English</option><option value="fr">Français</option><option value="es">Español</option><option value="it">Italiano</option><option value="de">Deutsch</option><option value="ru">Русский</option></select></div>
      <div class="def-list" id="def-list"></div>
      <p class="muted" style="margin-top:10px">${te('Your own definitions: put your .yml files in {dir}', { dir: defsCache.customDir })}</p>`;
    const torznab = `<div class="def" data-act="pick-torznab"><div class="grow"><div class="n">${esc(t('Generic Torznab'))}</div>
      <div class="d">${esc(t('For an indexer you already use through Jackett, Prowlarr or any Torznab-compatible service: paste its Torznab address and API key.'))}</div></div><span class="tag">Torznab</span></div>`;
    let lastKey = null;
    const draw = () => {
      const q = $('#def-q').value.toLowerCase(), ty = $('#def-type').value, l = $('#def-lang').value;
      const key = q + '|' + ty + '|' + l;
      if (key === lastKey) return; // "change" fires on blur: do not re-render under the pointer
      lastKey = key;
      const list = defs.filter((d) => (!q || (d.name + ' ' + d.description + ' ' + d.id).toLowerCase().includes(q)) && (!ty || d.type === ty) &&
        (!l || (d.language || '').toLowerCase().startsWith(l)));
      const showTorznab = !ty && !l && (!q || 'torznab jackett prowlarr'.includes(q) || q.includes('torznab'));
      $('#def-list').innerHTML = (showTorznab ? torznab : '') + (list.slice(0, 300).map((d) => `<div class="def" data-act="pick-def" data-id="${esc(d.id)}">
        <div class="grow"><div class="n">${esc(d.name)}</div><div class="d">${esc(d.description || '')}</div></div>
        <span class="tag">${esc(d.language || '')}</span><span class="tag ${d.type === 'public' ? 'g' : d.type === 'private' ? 'r' : ''}">${esc(d.type)}</span></div>`).join('') ||
        (showTorznab ? '' : `<div class="empty">${esc(t('No site matches.'))}</div>`));
    };
    ['input', 'change'].forEach((ev) => m.body.addEventListener(ev, (e) => { if (e.target.closest('#def-q,#def-type,#def-lang')) draw(); }));
    draw();
    $('#def-q').focus();
    m.body.addEventListener('click', (e) => {
      const d = e.target.closest('[data-act="pick-def"]');
      if (d) { e.stopPropagation(); m.close(); indexerConfigModal({ definitionId: d.dataset.id, kind: 'cardigann', enabled: true, priority: 25 }); }
      if (e.target.closest('[data-act="pick-torznab"]')) { e.stopPropagation(); m.close(); indexerConfigModal({ kind: 'torznab', enabled: true, priority: 25 }); }
    });
  } catch (e) { m.body.innerHTML = `<p class="bad-text">${esc(e.message)}</p>`; }
}

function settingField(f, value) {
  const id = 'set-' + f.name;
  const v = value ?? f.default ?? '';
  switch (f.type) {
    case 'info': case 'info_cookie': case 'info_flaresolverr': case 'info_useragent': case 'info_category_8000':
      return `<div class="callout">${f.label ? `<b>${esc(f.label)}</b><br>` : ''}${esc(String(f.default || '').replace(/<[^>]+>/g, ' '))}</div>`;
    case 'checkbox':
      return `<label class="check"><input type="checkbox" data-setting="${esc(f.name)}" ${String(v) === 'true' ? 'checked' : ''}>${esc(f.label || f.name)}</label>`;
    case 'select': {
      const opts = Object.entries(f.options || {});
      return `<div class="field"><label for="${esc(id)}">${esc(f.label || f.name)}</label><select id="${esc(id)}" data-setting="${esc(f.name)}">
        ${opts.map(([k, lab]) => `<option value="${esc(k)}" ${String(v) === k ? 'selected' : ''}>${esc(lab)}</option>`).join('')}</select></div>`;
    }
    case 'multi-select': {
      const sel = String(value ?? (f.defaults || []).join(',')).split(',');
      return `<div class="field"><label>${esc(f.label || f.name)}</label><div class="qlist">${Object.entries(f.options || {}).map(([k, lab]) =>
        `<label class="check"><input type="checkbox" data-multi="${esc(f.name)}" value="${esc(k)}" ${sel.includes(k) ? 'checked' : ''}>${esc(lab)}</label>`).join('')}</div></div>`;
    }
    default:
      return `<div class="field"><label for="${esc(id)}">${esc(f.label || f.name)}</label>
        <input id="${esc(id)}" data-setting="${esc(f.name)}" type="${f.type === 'password' ? 'password' : 'text'}" value="${esc(v)}" autocomplete="off"></div>`;
  }
}

function readSettingFields(root) {
  const out = {};
  $$('[data-setting]', root).forEach((el) => { out[el.dataset.setting] = el.type === 'checkbox' ? String(el.checked) : el.value; });
  const multi = {};
  $$('[data-multi]', root).forEach((el) => { multi[el.dataset.multi] = multi[el.dataset.multi] || []; if (el.checked) multi[el.dataset.multi].push(el.value); });
  Object.entries(multi).forEach(([k, v]) => { out[k] = v.join(','); });
  return out;
}

async function indexerConfigModal(cfg) {
  const m = modal({ title: t('Configure the indexer'), body: `<div class="loading"><span class="spinner"></span>${esc(t('Loading the definition…'))}</div>`,
    footer: `<span id="cfg-result" class="muted spacer"></span><button data-test>${esc(t('Test'))}</button><button class="primary" data-save>${esc(t('Save'))}</button>` });
  let def = null;
  try {
    if (cfg.kind === 'torznab') {
      m.el.querySelector('h2').textContent = t('Generic Torznab');
      m.body.innerHTML = `<p class="muted" style="margin-top:0">${esc(t('Torznab is the standard protocol of Jackett and Prowlarr: if you already configured a site there, Cinearr can query it through them.'))}</p>
        <div class="field"><label>${esc(t('Name'))}</label><input id="cfg-name" value="${esc(cfg.name || 'Torznab')}"></div>
        <div class="field"><label>${esc(t('Torznab address'))}</label><input id="cfg-url" value="${esc(cfg.baseUrl || '')}" placeholder="http://127.0.0.1:9117/api/v2.0/indexers/xxx/results/torznab/">
        <div class="hint">${esc(t('Jackett: “Copy Torznab Feed” button. Prowlarr: indexer › “Torznab URL” (e.g. http://localhost:9696/1/api).'))}</div></div>
        <div class="field"><label>${esc(t('API key'))}</label><input id="cfg-key" value="${esc(cfg.apiKey || '')}" autocomplete="off"></div>
        <div class="field"><label>${esc(t('Priority'))}</label><input id="cfg-prio" type="number" min="1" max="50" value="${cfg.priority || 25}"><div class="hint">${esc(t('1 = preferred, 50 = last resort.'))}</div></div>
        <label class="check"><input type="checkbox" id="cfg-noproxy" ${cfg.noProxy ? 'checked' : ''}>${esc(t('Connect directly, without the proxy'))}</label><div class="hint muted" style="margin:-4px 0 10px 30px;font-size:12.5px">${esc(t('For a site that refuses VPN addresses (some private trackers). Only useful when a proxy is set in Settings › General.'))}</div>`;
    } else {
      def = await api('GET', '/indexers/definitions/' + encodeURIComponent(cfg.definitionId));
      m.el.querySelector('h2').textContent = def.name;
      const links = def.links || [];
      m.body.innerHTML = `<p class="muted" style="margin-top:0">${esc(def.description || '')}</p>
        <div class="row" style="margin-bottom:14px"><span class="tag ${def.type === 'public' ? 'g' : 'r'}">${esc(def.type)}</span><span class="tag">${esc(def.language || '')}</span>
          ${def.imdbSearch ? `<span class="tag g">${esc(t('IMDb search'))}</span>` : `<span class="tag">${esc(t('title search'))}</span>`}</div>
        ${def.captcha ? `<div class="callout warn">${esc(t('This site shows a captcha at login: use the session cookie if the definition allows it.'))}</div>` : ''}
        <div class="field"><label>${esc(t('Display name'))}</label><input id="cfg-name" value="${esc(cfg.name || def.name)}"></div>
        <div class="field"><label for="cfg-url">${esc(t('Site address'))}</label>
          <input id="cfg-url" list="cfg-links" value="${esc(cfg.baseUrl || links[0] || '')}" autocomplete="off">
          <datalist id="cfg-links">${links.map((l) => `<option value="${esc(l)}">`).join('')}</datalist>
          <div class="hint">${esc(t('Known mirrors:'))} ${links.map((l) => `<a href="#" data-mirror="${esc(l)}">${esc(l.replace(/^https?:\/\//, '').replace(/\/$/, ''))}</a>`).join(' · ')}.
          ${esc(t('You can also type another address (for example the one used in Prowlarr or Jackett).'))}</div></div>
        ${(def.settings || []).map((f) => settingField(f, cfg.settings?.[f.name])).join('')}
        <div class="field"><label>${esc(t('Priority'))}</label><input id="cfg-prio" type="number" min="1" max="50" value="${cfg.priority || 25}"><div class="hint">${esc(t('1 = preferred, 50 = last resort (breaks ties between equivalent releases).'))}</div></div>
        <label class="check"><input type="checkbox" id="cfg-noproxy" ${cfg.noProxy ? 'checked' : ''}>${esc(t('Connect directly, without the proxy'))}</label><div class="hint muted" style="margin:-4px 0 10px 30px;font-size:12.5px">${esc(t('For a site that refuses VPN addresses (some private trackers). Only useful when a proxy is set in Settings › General.'))}</div>`;
    }
  } catch (e) { m.body.innerHTML = `<p class="bad-text">${esc(e.message)}</p>`; return; }
  const collect = () => {
    const c = { ...cfg, name: $('#cfg-name', m.body).value.trim(), baseUrl: $('#cfg-url', m.body).value.trim(), priority: +$('#cfg-prio', m.body).value || 25, noProxy: !!$('#cfg-noproxy', m.body)?.checked };
    if (cfg.kind === 'torznab') c.apiKey = $('#cfg-key', m.body).value.trim();
    else c.settings = readSettingFields(m.body);
    if (c.baseUrl && !/^https?:\/\//i.test(c.baseUrl)) c.baseUrl = 'https://' + c.baseUrl;
    if (c.baseUrl && !c.baseUrl.endsWith('/') && cfg.kind !== 'torznab') c.baseUrl += '/';
    if (def && c.baseUrl === (def.links || [])[0]) c.baseUrl = '';
    return c;
  };
  m.body.addEventListener('click', (e) => {
    const a = e.target.closest('[data-mirror]');
    if (a) { e.preventDefault(); $('#cfg-url', m.body).value = a.dataset.mirror; }
  });
  const res = $('#cfg-result', m.el);
  m.el.querySelector('[data-test]').onclick = async (e) => {
    const btn = e.currentTarget;
    btn.disabled = true;
    res.innerHTML = `<span class="spinner"></span> ${esc(t('Testing…'))}`;
    try { const r = await api('POST', '/indexers/test', collect()); res.innerHTML = `<span class="ok-text">✓ ${esc(plural(r.results, '{n} result', '{n} results'))}</span>`; }
    catch (err) { res.innerHTML = `<span class="bad-text">${esc(err.message)}</span>`; }
    btn.disabled = false;
  };
  m.el.querySelector('[data-save]').onclick = async () => {
    try { await api('POST', '/indexers', collect()); toast(t('Indexer saved'), 'ok'); m.close(); refreshStatus(); drawIndexers(); }
    catch (err) { fail(err); }
  };
}

/* ----------------------------------------------------------------- activity */
let activityTab = 'history';
function viewActivity(v) {
  v.innerHTML = `<div class="page"><div class="page-head"><h1>${esc(t('Activity'))}</h1><div class="chips">
    <span class="chip ${activityTab === 'history' ? 'active' : ''}" data-act="act-tab" data-t="history">${esc(t('History'))}</span>
    <span class="chip ${activityTab === 'blocklist' ? 'active' : ''}" data-act="act-tab" data-t="blocklist">${esc(t('Blocklist'))}</span>
    <span class="chip ${activityTab === 'log' ? 'active' : ''}" data-act="act-tab" data-t="log">${esc(t('Log'))}</span></div></div><div id="act"></div></div>`;
  if (activityTab === 'log') { drawLog(true); state.refresh = () => drawLog(false); return; }
  drawActivity();
}

let logErrorsOnly = false;
const LOG_BAD = /PANIC|\[api\]|\[http\]|\[indexer\]|\[interface\]|error|failed|impossible|unreachable|refused|not readable|could not/i;
async function drawLog(first) {
  const box = $('#act');
  if (!box) return;
  if (first) {
    box.innerHTML = `<div class="row" style="flex-wrap:wrap;margin-bottom:12px">
      <label class="check" style="margin:0"><input type="checkbox" id="log-errors" ${logErrorsOnly ? 'checked' : ''}>${esc(t('Errors only'))}</label>
      <label class="check" style="margin:0 0 0 14px" title="${esc(t('Records every request to the sites, TMDB and the torrent client (passwords and keys hidden)'))}"><input type="checkbox" id="log-debug" ${state.status?.debug ? 'checked' : ''}>${esc(t('Detailed log'))}</label>
      <span class="spacer"></span><button data-act="open-logs">${ICON.folder}${esc(t('Open the folder'))}</button>
      <a class="btn primary" href="/api/diagnostic" download data-act-dl>${ICON.downloads}${esc(t('Export a diagnostic'))}</a></div>
      <pre class="logview" id="logview">${esc(t('Loading…'))}</pre><div class="muted" id="logfile" style="margin-top:8px"></div>`;
    $('#log-errors').addEventListener('change', (e) => { logErrorsOnly = e.target.checked; drawLog(false); });
    $('#log-debug').addEventListener('change', async (e) => {
      try { await api('POST', '/debug', { on: e.target.checked }); toast(e.target.checked ? t('Detailed log enabled') : t('Detailed log disabled')); } catch (err) { fail(err); }
    });
    box.querySelector('[data-act-dl]').addEventListener('click', () => toast(t('Diagnostic saved in your Downloads folder')));
  }
  try {
    const r = await api('GET', '/logs?lines=600');
    const pre = $('#logview');
    if (!pre) return;
    const atBottom = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 30;
    const isDebug = (l) => /\[debug\]/.test(l) && !/PANIC/.test(l);
    const isBad = (l) => !isDebug(l) && LOG_BAD.test(l);
    const lines = logErrorsOnly ? r.lines.filter(isBad) : r.lines;
    pre.innerHTML = lines.length ? lines.map((l) => `<span class="${isBad(l) ? 'bad-text' : isDebug(l) ? 'dimline' : /\[warning\]/.test(l) ? 'warn-text' : ''}">${esc(l)}</span>`).join('\n') : esc(t('No line.'));
    $('#logfile').textContent = t('File: {path}', { path: r.file });
    if (first || atBottom) pre.scrollTop = pre.scrollHeight;
  } catch (e) { const pre = $('#logview'); if (pre) pre.textContent = e.message; }
}

async function drawActivity() {
  const box = $('#act');
  setLoading(box);
  try {
    if (activityTab === 'history') {
      const h = await api('GET', '/history');
      box.innerHTML = h.length ? `<div class="card" style="padding:6px 0"><table><tbody>${h.map((e) => `<tr><td style="width:150px" class="muted">${date(e.date)}</td>
        <td style="width:110px">${kindTag(e.kind)}</td>
        <td>${e.tmdbId ? `<a href="#/movie/${e.tmdbId}">${esc(e.message)}</a>` : e.seriesId ? `<a href="#/series/${e.seriesId}">${esc(e.message)}</a>` : esc(e.message)}</td></tr>`).join('')}</tbody></table></div>` :
        `<div class="empty">${ICON.activity}<h2>${esc(t('Nothing yet'))}</h2></div>`;
    } else {
      const b = await api('GET', '/blocklist');
      box.innerHTML = b.length ? `<div class="callout">${esc(t('The releases below will never be downloaded again (dangerous content, failure, or manual removal).'))}</div>
        <div class="card" style="padding:6px 0"><table><thead><tr><th>${esc(t('Date'))}</th><th>Release</th><th>${esc(t('Reason'))}</th><th></th></tr></thead><tbody>${b.map((e, i) => `<tr><td class="muted" style="width:140px">${date(e.date)}</td>
        <td class="rel-title">${esc(e.title)}<div class="muted">${esc(e.indexer || '')}</div></td><td>${esc(e.reason)}</td>
        <td><button class="small ghost" data-act="unblock" data-i="${i}">${esc(t('Remove'))}</button></td></tr>`).join('')}</tbody></table></div>` :
        `<div class="empty">${ICON.shield}<h2>${esc(t('Empty blocklist'))}</h2><p>${esc(t('Releases refused for safety reasons will appear here.'))}</p></div>`;
    }
  } catch (e) { box.innerHTML = `<p class="bad-text">${esc(e.message)}</p>`; }
}

/* ----------------------------------------------------------------- settings */
// languages of the film pages (TMDB)
const LANGS = [['en-US', 'English (US)'], ['en-GB', 'English (UK)'], ['fr-FR', 'Français'], ['fr-CA', 'Français (Canada)'], ['es-ES', 'Español (España)'], ['es-MX', 'Español (México)'],
  ['it-IT', 'Italiano'], ['de-DE', 'Deutsch'], ['pt-BR', 'Português (Brasil)'], ['pt-PT', 'Português'], ['nl-NL', 'Nederlands'], ['pl-PL', 'Polski'], ['ru-RU', 'Русский'], ['ar-SA', 'العربية'], ['ja-JP', '日本語']];
const list = (s) => String(s || '').split(',').map((x) => x.trim()).filter(Boolean);
const fieldId = (path) => 'f-' + path.replace(/\./g, '-');

function getPath(obj, path) { return path.split('.').reduce((o, k) => (o == null ? undefined : o[k]), obj); }
function setPath(obj, path, v) {
  const keys = path.split('.');
  const last = keys.pop();
  const o = keys.reduce((x, k) => (x[k] = x[k] || {}), obj);
  o[last] = v;
}

// field renders an input bound to a settings path (read back by readBinds).
function field(obj, path, label, hint = '', type = 'text', extra = '') {
  let v = getPath(obj, path);
  if (type === 'list' || type === 'lower-list') v = (v || []).join(', ');
  const inputType = type === 'number' ? 'number' : type === 'password' ? 'password' : 'text';
  return `<div class="field"><label for="${fieldId(path)}">${esc(label)}</label><div class="row"><input id="${fieldId(path)}" type="${inputType}" data-bind="${esc(path)}" data-kind="${type}" value="${esc(v ?? '')}" autocomplete="off">${extra}</div>${hint ? `<div class="hint">${hint}</div>` : ''}</div>`;
}
function check(obj, path, label, hint = '') {
  return `<label class="check"><input type="checkbox" id="${fieldId(path)}" data-bind="${esc(path)}" data-kind="bool" ${getPath(obj, path) ? 'checked' : ''}>${esc(label)}</label>${hint ? `<div class="hint muted" style="margin:-4px 0 10px 30px;font-size:12.5px">${esc(hint)}</div>` : ''}`;
}
function select(obj, path, label, options, kind = 'text', hint = '') {
  const v = String(getPath(obj, path) ?? '');
  return `<div class="field"><label for="${fieldId(path)}">${esc(label)}</label><select id="${fieldId(path)}" data-bind="${esc(path)}" data-kind="${kind}">
    ${options.map(([k, l]) => `<option value="${esc(k)}" ${v === String(k) ? 'selected' : ''}>${esc(l)}</option>`).join('')}</select>${hint ? `<div class="hint">${esc(hint)}</div>` : ''}</div>`;
}
function readBinds(obj, root = document) {
  $$('[data-bind]', root).forEach((el) => {
    const k = el.dataset.kind;
    let v = el.value;
    if (k === 'bool') v = el.checked;
    else if (k === 'number') v = +el.value || 0;
    else if (k === 'list') v = list(el.value);
    else if (k === 'lower-list') v = list(el.value).map((x) => x.toLowerCase());
    else if (k !== 'password') v = el.value.trim();
    setPath(obj, el.dataset.bind, v);
  });
}

let edit = null; // settings being edited (all tabs)
let settingsTab = 'general';
const SETTINGS_TABS = () => [['general', t('General')], ['client', t('Torrent client')], ['library', t('Library')], ['quality', t('Quality')],
  ['subtitles', t('Subtitles')], ['player', t('Player')], ['data', t('Data and diagnostics')]];

async function viewSettings(v, tab) {
  setLoading(v);
  if (tab && SETTINGS_TABS().some(([k]) => k === tab)) settingsTab = tab;
  try {
    const [s, q] = await Promise.all([api('GET', '/settings'), state.qualities.length ? state.qualities : api('GET', '/qualities'), loadClients()]);
    state.settings = s;
    state.qualities = q;
    edit = JSON.parse(JSON.stringify(s));
  } catch (e) { v.innerHTML = `<div class="page"><p class="bad-text">${esc(e.message)}</p></div>`; return; }
  editClients(edit);
  plEdit = edit.player;
  subEdit = edit.subtitles;
  v.innerHTML = `<div class="page"><div class="page-head"><h1>${esc(t('Settings'))}</h1><div class="spacer"></div><button class="primary" data-act="save-settings">${esc(t('Save'))}</button></div>
    <div class="tabs" role="tablist">${SETTINGS_TABS().map(([k, l]) => `<button role="tab" class="tab ${k === settingsTab ? 'active' : ''}" aria-selected="${k === settingsTab}" data-act="set-tab" data-tab="${k}">${esc(l)}</button>`).join('')}</div>
    <div id="set-body" class="set-body"></div></div>`;
  drawSettingsTab();
}

// readTab copies the fields of the visible tab into the edited settings.
function readTab() {
  if (!edit || !$('#set-body')) return edit;
  readBinds(edit, $('#set-body'));
  if ($('#dc-url')) edit.downloadClient = readClientForm(dcEdit);
  if ($('#player-box')) readPlayerForm(plEdit);
  if ($('[data-q]')) {
    const allowed = $$('[data-q]').filter((e) => e.checked).map((e) => +e.dataset.q);
    const order = [...state.qualities].sort((a, b) => a.weight - b.weight).map((d) => d.quality.id);
    allowed.sort((a, b) => order.indexOf(a) - order.indexOf(b));
    edit.profile.allowed = allowed;
    if (!allowed.includes(edit.profile.cutoff) && allowed.length) edit.profile.cutoff = allowed[allowed.length - 1];
  }
  if ($('#l-maps')) {
    edit.pathMappings = $('#l-maps').value.split('\n').map((l) => l.split('=>').map((x) => x.trim())).filter((x) => x.length === 2 && x[0] && x[1]).map(([remote, local]) => ({ remote, local }));
  }
  if ($('#p-terms')) {
    edit.profile.preferred = $('#p-terms').value.split('\n').map((l) => l.split('=')).filter((x) => x[0].trim()).map(([term, sc]) => ({ term: term.trim(), score: parseInt(sc, 10) || 0 }));
  }
  return edit;
}

function drawSettingsTab() {
  const box = $('#set-body');
  if (!box) return;
  const s = edit;
  const p = s.profile || {};
  let html = '';
  switch (settingsTab) {
    case 'general':
      html = `<div class="card"><h2>${esc(t('Interface'))}</h2>
        ${select(s, 'uiLanguage', t('Language of the interface'), UI_LANGUAGES)}
        </div>
        <div class="card"><h2>${esc(t('Film information'))}</h2>${keyProblem('general')}
        ${field(s, 'tmdbKey', t('TMDB key'), state.status?.tmdbBuiltin
    ? t('Optional: Cinearr provides its own key. If it ever stops working, enter yours here (API v3 key or v4 read access token, free on <a href="https://www.themoviedb.org/settings/api" target="_blank" rel="noopener">themoviedb.org</a>).')
    : t('API v3 key or v4 read access token, free on <a href="https://www.themoviedb.org/settings/api" target="_blank" rel="noopener">themoviedb.org</a>. TMDB also provides the IMDb ids.'), 'password')}
        ${select(s, 'language', t('Language of the film pages'), LANGS)}
        </div>
        <div class="card"><h2>${esc(t('Proxy (VPN)'))}</h2><p class="card-sub">${esc(t('To reach sites blocked in your country, the searches can go through a proxy: most VPN services provide one (SOCKS5 or HTTP), with its address, port and account in their member area. Site names are resolved by the proxy. If your VPN already protects the whole computer, leave this empty.'))}</p>
        ${select(s, 'proxy.type', t('Proxy'), [['', t('None (direct connection)')], ['socks5', 'SOCKS5'], ['http', 'HTTP / HTTPS']])}
        <div class="grid2">
          ${field(s, 'proxy.host', t('Address'), esc(t('E.g. nl.socks.example.com')))}
          ${field(s, 'proxy.port', t('Port'), esc(t('Often 1080 for SOCKS5')), 'number')}
          ${field(s, 'proxy.username', t('User name'), '')}
          ${field(s, 'proxy.password', t('Password'), '', 'password')}
        </div>
        ${check(s, 'proxy.indexers', t('Indexers: searches and .torrent files'), t('Sites can be excluded one by one in their settings (Indexers page).'))}
        ${check(s, 'proxy.metadata', t('Also TMDB and the subtitle providers'))}
        <div class="row"><button class="small" data-act="proxy-test">${esc(t('Test the proxy'))}</button><span id="proxy-test" class="muted"></span></div>
        <p class="hint muted" style="margin-top:10px">${esc(t('The proxy is used by Cinearr only. The downloads themselves are made by your torrent client: to hide them too, set the VPN or the proxy in the client.'))}</p>
        </div>
        <div class="card"><h2>${esc(t('Downloads'))}</h2>
        ${check(s, 'searchOnAdd', t('Search and download as soon as a film is added'), t('Off: the film is only added to the library; start the automatic or manual search from its page.'))}
        ${check(s, 'autoGrab', t('When a download fails, look for another release'))}
        </div>`;
      break;
    case 'client':
      html = `<div class="card"><h2>${esc(t('Torrent client'))}</h2><p class="card-sub">${esc(t('The software that downloads the torrents. Cinearr drives it through its web interface.'))}</p>
        <div id="dc-form">${clientForm(dcEdit, true)}</div></div>`;
      break;
    case 'library':
      html = `<div class="card"><h2>${esc(t('Library'))}</h2>
        ${field(s, 'libraryPath', t('Films folder'), esc(t('Each film is stored in “Title (Year)”.')), 'text', `<button data-act="pick" data-target="${fieldId('libraryPath')}" aria-label="${esc(t('Browse'))}">${ICON.folder}</button>`)}
        ${field(s, 'seriesPath', t('Series folder'), esc(t('Each series is stored in “Title (Year)/Season 01”.')), 'text', `<button data-act="pick" data-target="${fieldId('seriesPath')}" aria-label="${esc(t('Browse'))}">${ICON.folder}</button>`)}
        <div class="callout">${esc(t('Cinearr reads these folders at each launch: films (in a “Title (Year)” folder or directly in the films folder) and series (one folder per series) added by hand appear in the library. When you change a folder, the paths of the library follow it: move the files yourself.'))}
          <div style="margin-top:10px"><button class="small" data-act="scan">${ICON.refresh}${esc(t('Update the library'))}</button></div></div>
        ${select(s, 'importMode', t('Import'), [['hardlink', t('Hard link (recommended: keeps seeding without using twice the space; copy if the disks differ)')], ['copy', t('Copy')], ['move', t('Move (stops seeding)')]])}
        ${check(s, 'removeAfterImport', t('Remove the torrent from the client once it has finished seeding'))}
        <div class="field"><label for="l-maps">${esc(t('Path mappings (torrent client on another machine)'))}</label>
          <textarea id="l-maps" placeholder="/downloads => D:\\Torrents">${esc((s.pathMappings || []).map((m) => `${m.remote} => ${m.local}`).join('\n'))}</textarea>
          <div class="hint">${esc(t('One mapping per line: path seen by the torrent client => local path.'))}</div></div></div>
        <div class="card"><h2>${esc(t('Import checks'))}</h2><p class="card-sub">${esc(t('For films and series. Executables, shortcuts and scripts (.exe, .bat, .lnk, .scr, .vbs…) are always refused.'))}</p>
        <div class="grid2">
          ${field(s, 'rejectedExtensions', t('Other refused file extensions'), esc(t('Comma separated, e.g. .url, .iso, .wmv: a download containing such a file is refused and its release blocklisted.')), 'lower-list')}
          ${field(s, 'minFreeSpaceMB', t('Free space to keep (MB)'), esc(t('A download or a copy into the films or series folder must leave this space free. 0 = only check that the file fits.')), 'number')}
          ${field(s, 'recycleDays', t('Recycle bin (days)'), esc(t('Files removed from the library or replaced by a better release are kept this many days in the hidden folder “.cinearr-recycle” of the films or series folder. 0 = deleted at once.')), 'number')}
        </div></div>`;
      break;
    case 'quality': {
      const quals = [...state.qualities].sort((a, b) => a.weight - b.weight);
      html = `<div class="card"><h2>${esc(t('Quality'))}</h2><p class="card-sub">${esc(t('Taken from Radarr’s quality profile: qualities are ordered from worst to best.'))}</p>
        <div class="row" style="flex-wrap:wrap;margin-bottom:14px"><span class="muted">${esc(t('Radarr profiles:'))}</span>
          ${Object.keys(RADARR_PROFILES).map((k) => `<span class="chip" data-act="preset" data-p="${esc(k)}">${esc(k)}</span>`).join('')}</div>
        <div class="qlist">${quals.map((d) => `<label class="check"><input type="checkbox" data-q="${d.quality.id}" ${(p.allowed || []).includes(d.quality.id) ? 'checked' : ''}>${esc(d.quality.name)}</label>`).join('')}</div>
        <div class="grid2" style="margin-top:14px">
          ${select(s, 'profile.cutoff', t('Target quality (no upgrade beyond)'), quals.map((d) => [d.quality.id, d.quality.name]), 'number')}
          ${field(s, 'profile.minSeeders', t('Minimum seeds'), '', 'number')}
          ${field(s, 'profile.maxSizeGB', t('Maximum size (GB, 0 = unlimited)'), '', 'number')}
          ${field(s, 'profile.preferredLanguages', t('Preferred languages'), esc(t('English names, in order: French, English…')), 'list')}
          ${field(s, 'profile.requiredLanguages', t('Required languages (empty = any)'), esc(t('E.g. French: refuses releases without a French track.')), 'list')}
          ${field(s, 'profile.multiCountsAs', t('A “MULTi” release contains'), esc(t('On French sites, MULTi = French + original.')), 'list')}
          ${field(s, 'profile.mustContain', t('Must contain (one of the terms)'), esc(t('Comma separated; /regex/ accepted.')), 'list')}
          ${field(s, 'profile.mustNotContain', t('Must not contain'), esc(t('E.g. HDCAM, TS, /\\bCAM\\b/')), 'list')}
        </div>
        <div class="field"><label for="p-terms">${esc(t('Preferred terms (one per line: term = points)'))}</label><textarea id="p-terms">${esc((p.preferred || []).map((x) => `${x.term} = ${x.score}`).join('\n'))}</textarea></div>
        ${check(s, 'profile.upgradesAllowed', t('Replace automatically with a better quality'))}
        ${check(s, 'profile.rejectHardcodedSubs', t('Refuse hardcoded subtitles'))}
        ${check(s, 'profile.antiFakeMinSize', t('Anti-fake: refuse releases abnormally small for their quality'))}</div>`;
      break;
    }
    case 'subtitles':
      html = `<div class="card"><h2>${esc(t('Subtitles'))}</h2><p class="card-sub">${esc(t('Search and score taken from Bazarr.'))}</p>
        ${check(s, 'subtitles.auto', t('Download automatically after the import'))}
        <div class="grid2">${field(s, 'subtitles.languages', t('Languages (ISO codes, in order)'), esc(t('E.g. en, fr')), 'lower-list')}
        ${field(s, 'subtitles.minPercent', t('Minimum score (%)'), esc(t('Below it, nothing is downloaded automatically.')), 'number')}</div>
        ${check(s, 'subtitles.hearingImpaired', t('Prefer subtitles for the deaf and hard of hearing'))}</div>
        <div class="card"><h2>${esc(t('Providers'))}</h2><p class="card-sub">${esc(t('The sites where subtitles are searched. Add the ones you want, with their account or key when they need one.'))}</p>${keyProblem('subtitles')}
        <div id="sub-providers"></div></div>`;
      break;
    case 'player':
      html = `<div class="card"><h2>${esc(t('Player'))}</h2><p class="card-sub">${esc(t('The software that opens your films. If it is not installed, Cinearr can download it for you.'))}</p>
        <div id="player-box"></div>
        <div class="grid2" style="margin-top:14px">${field(s, 'player.audioLang', t('Preferred audio languages'), esc(t('Comma-separated codes (eng,en,fre,fr). VLC and mpv; MPC-HC is set in its own options.')))}
        ${field(s, 'player.extraArgs', t('Extra player options'), esc(t('Added to the command line (advanced).')))}</div>
        ${check(s, 'player.fullscreen', t('Open in full screen'))}${check(s, 'player.resume', t('Resume where I stopped'))}</div>`;
      break;
    case 'data':
      html = `<div class="card"><h2>${esc(t('Data and diagnostics'))}</h2><p class="muted">${te('Folder: {dir} (settings, library, definition cache, log).', { dir: state.status?.dataDir || '' })}</p>
        ${check(s, 'debug', t('Detailed log'), t('Records every request to the sites, TMDB, the torrent client and the subtitle providers (passwords and keys hidden). Enable it to analyse a problem.'))}
        <div class="row" style="flex-wrap:wrap;margin:6px 0 14px"><a class="btn" href="#/activity" data-act="show-log">${ICON.activity}${esc(t('Show the log'))}</a><button data-act="open-logs">${ICON.folder}${esc(t('Open the folder'))}</button>
          <a class="btn" href="/api/diagnostic" download>${ICON.downloads}${esc(t('Export a diagnostic'))}</a></div>
        <p class="muted" style="margin-bottom:0">${esc(t('This product uses the TMDB API but is not endorsed or certified by TMDB. Indexer definitions: Prowlarr project.'))}</p></div>
        <div class="card"><h2>${esc(t('Version'))}</h2>
        <div class="row" style="flex-wrap:wrap"><span>Cinearr ${esc(state.status?.version || '')}</span><button class="small" data-act="check-update">${ICON.refresh}${esc(t('Check for updates'))}</button><span id="update-result" class="muted"></span></div></div>
        <div class="card danger-zone"><h2>${esc(t('Reset'))}</h2>
        <p class="muted">${esc(t('Erases every setting and empties the library lists, then starts the setup wizard again, to configure Cinearr from scratch. No file is deleted: the films and series of the folders you choose are added back.'))}</p>
        <button class="danger" data-act="reset-app">${ICON.refresh}${esc(t('Reset Cinearr'))}</button></div>`;
      break;
  }
  box.innerHTML = html;
  if (settingsTab === 'player') drawPlayers();
  if (settingsTab === 'subtitles') drawSubProviders();
}

async function saveSettings(quiet) {
  readTab();
  const before = state.settings?.uiLanguage || 'en';
  const beforePages = state.settings?.language;
  const saved = await api('PUT', '/settings', edit);
  state.settings = saved;
  edit = JSON.parse(JSON.stringify(saved));
  editClients(edit);
  plEdit = edit.player;
  subEdit = edit.subtitles;
  if ((saved.uiLanguage || 'en') !== before) {
    await setLanguage(saved.uiLanguage);
    await refreshStatus();
    viewSettings($('#view'), settingsTab);
  } else if (!quiet) {
    drawSettingsTab();
  }
  if (beforePages && saved.language !== beforePages && ((state.movies || []).length || (state.series || []).length)) {
    toast(t('The film and series pages of your library are being reloaded in the new language.'), 'ok');
  }
  refreshStatus();
}

// Radarr's default quality profiles (QualityProfileService.AddDefaultProfile).
const RADARR_PROFILES = {
  'Any': { cutoff: 20, ids: [24, 25, 26, 27, 28, 29, 1, 2, 23, 4, 9, 16, 8, 12, 5, 14, 3, 15, 18, 17, 20, 21, 6, 7, 19, 30, 31, 22] },
  'SD': { cutoff: 20, ids: [24, 25, 26, 27, 28, 29, 1, 2, 8, 12, 20, 21] },
  'HD-720p': { cutoff: 6, ids: [4, 5, 14, 6] },
  'HD-1080p': { cutoff: 7, ids: [9, 3, 15, 7, 30] },
  'Ultra-HD': { cutoff: 31, ids: [16, 18, 17, 19, 31] },
  'HD - 720p/1080p': { cutoff: 6, ids: [4, 9, 5, 14, 3, 15, 6, 7, 30] },
};

/* ----------------------------------------------------------- torrent client */
let CLIENTS = [];
let dcEdit = null; // client being edited (settings page or wizard draft)
let dcOthers = {}; // settings of the clients not selected, by kind (kept when switching)

// editClients starts editing the torrent client of a settings object.
function editClients(s) {
  dcEdit = s.downloadClient;
  if (!s.otherClients) s.otherClients = {};
  dcOthers = s.otherClients;
}
async function loadClients() {
  if (!CLIENTS.length) {
    try { CLIENTS = await api('GET', '/download-clients'); } catch (e) { CLIENTS = [{ id: 'qbittorrent', name: 'qBittorrent', defaultUrl: 'http://127.0.0.1:8080', username: true, savePath: true, sequential: true, timeLimit: true, help: '' }]; }
  }
  return CLIENTS;
}
const clientInfo = (kind) => CLIENTS.find((c) => c.id === kind) || CLIENTS[0] || {};

function clientForm(c, full) {
  const info = clientInfo(c.kind);
  const catLabel = info.id === 'qbittorrent' ? t('Category') : t('Label');
  const catHint = info.id === 'transmission' ? t('A label with Transmission 4; a sub-folder with older versions.') :
    info.id === 'deluge' ? t('Needs Deluge’s “Label” plugin (lower case only).') : t('Handy to find Cinearr’s torrents in the client.');
  return `<div class="choice" role="radiogroup" aria-label="${esc(t('Torrent client'))}">${CLIENTS.map((x) => `<button type="button" role="radio" aria-checked="${x.id === info.id}" class="choice-item ${x.id === info.id ? 'active' : ''}" data-act="dc-kind" data-kind="${esc(x.id)}">${esc(x.name)}</button>`).join('')}</div>
    <div class="callout">${esc(t(info.help || ''))}</div>
    <div class="grid2">${inputRow('dc-url', t('Web interface address'), c.url, te('Default: {url}', { url: info.defaultUrl || '' }))}
    ${full ? inputRow('dc-cat', catLabel, c.category, esc(catHint)) : ''}
    ${info.username ? inputRow('dc-user', t('Username'), c.username) : ''}${inputRow('dc-pass', t('Password'), c.password, info.id === 'deluge' ? esc(t('Default: deluge')) : '', 'password')}
    ${full && info.savePath ? inputRow('dc-save', t('Download folder (optional)'), c.savePath, te('Empty = default folder of {name}.', { name: info.name }), 'text', `<button data-act="pick" data-target="dc-save" aria-label="${esc(t('Browse'))}">${ICON.folder}</button>`) : ''}
    ${full ? `<div class="field"><label>${esc(t('Seeding after the download'))}</label><div class="row"><input id="dc-ratio" type="number" step="0.1" value="${c.ratioLimit}" title="${esc(t('Ratio (-2 = client setting, -1 = unlimited)'))}">
      ${info.timeLimit ? `<input id="dc-time" type="number" value="${c.seedingTimeLimit}" title="${esc(t('Minutes (-2 = client setting, -1 = unlimited)'))}">` : ''}</div>
      <div class="hint">${te(info.timeLimit ? 'Ratio and duration in minutes (-2 = setting of {name}, -1 = unlimited).' : 'Ratio (-2 = setting of {name}, -1 = unlimited).', { name: info.name })}</div></div>` : ''}</div>
    ${info.sequential ? checkRow('dc-seq', t('Sequential download (lets you start watching before the end)'), c.sequential) : ''}
    <div class="row" style="margin-top:6px"><button data-act="test-client">${esc(t('Test the connection'))}</button><span id="dc-res" class="muted"></span></div>`;
}

function inputRow(id, label, value, hint = '', type = 'text', extra = '') {
  return `<div class="field"><label for="${id}">${esc(label)}</label><div class="row"><input id="${id}" type="${type}" value="${esc(value ?? '')}" autocomplete="off">${extra}</div>${hint ? `<div class="hint">${hint}</div>` : ''}</div>`;
}
function checkRow(id, label, on, hint = '') {
  return `<label class="check"><input type="checkbox" id="${id}" ${on ? 'checked' : ''}>${esc(label)}</label>${hint ? `<div class="hint muted" style="margin:-4px 0 10px 30px;font-size:12.5px">${esc(hint)}</div>` : ''}`;
}

function readClientForm(c) {
  const el = (id) => $('#' + id);
  if (el('dc-url')) c.url = el('dc-url').value.trim();
  if (el('dc-cat')) c.category = el('dc-cat').value.trim() || 'cinearr';
  if (el('dc-user')) c.username = el('dc-user').value;
  if (el('dc-pass')) c.password = el('dc-pass').value;
  if (el('dc-save')) c.savePath = el('dc-save').value.trim();
  if (el('dc-ratio')) c.ratioLimit = +el('dc-ratio').value;
  if (el('dc-time')) c.seedingTimeLimit = +el('dc-time').value;
  if (el('dc-seq')) c.sequential = el('dc-seq').checked;
  return c;
}

// switchClient selects another torrent client. The settings of the one
// left are kept (and saved with the others), so that coming back to it
// brings back its address and account; a client never configured starts
// from its default address, with the category and seeding limits.
function switchClient(kind) {
  const c = readClientForm(dcEdit);
  if (c.kind === kind) return;
  dcOthers[c.kind] = { ...c };
  const next = dcOthers[kind] ? { ...dcOthers[kind] } :
    { ...c, url: clientInfo(kind).defaultUrl || '', username: '', password: '', savePath: '' };
  delete dcOthers[kind];
  for (const k of Object.keys(c)) delete c[k];
  Object.assign(c, next, { kind });
  const box = $('#dc-form');
  if (box) box.innerHTML = clientForm(c, state.view !== 'setup');
}

/* ------------------------------------------------------------------- player */
let plEdit = null; // player settings being edited
let playersInfo = null;
let playersTimer = null;

function readPlayerForm(p) {
  const el = $('#m-path');
  if (el) p.path = el.value.trim();
  return p;
}

async function drawPlayers() {
  const box = $('#player-box');
  if (!box) { clearTimeout(playersTimer); return; }
  readPlayerForm(plEdit);
  try {
    playersInfo = await api('GET', '/players?kind=' + encodeURIComponent(plEdit.kind || '') + '&path=' + encodeURIComponent(plEdit.path || ''));
  } catch (e) { box.innerHTML = `<p class="bad-text">${esc(e.message)}</p>`; return; }
  if (!$('#player-box')) return;
  const r = playersInfo;
  const kind = plEdit.kind || (plEdit.path ? 'custom' : '');
  const auto = !kind && r.active ? r.players.find((p) => p.path === r.active.path) : null;
  const card = (p) => {
    const sel = kind === p.id || (auto && auto.id === p.id);
    const ins = p.install || {};
    let status = p.path ? `<span class="tag g">${esc(t('Installed'))}</span>` : `<span class="tag">${esc(t('Not installed'))}</span>`;
    if (sel && !kind) status += ` <span class="tag q">${esc(t('chosen automatically'))}</span>`;
    let action = '';
    if (ins.running) {
      const pc = ins.total > 0 ? Math.floor(ins.done * 100 / ins.total) : 0;
      action = `<div class="muted" style="font-size:12.5px">${esc(t(ins.step || ''))}${ins.total > 0 ? ' · ' + esc(t('{p} % of {size}', { p: pc, size: size(ins.total) })) : ''}</div><div class="progress" style="margin-top:6px"><i style="width:${pc}%"></i></div>`;
    } else if (!p.path && p.canInstall) {
      action = `<button class="${sel ? 'primary' : ''} small" data-act="player-install" data-id="${esc(p.id)}">${ICON.downloads}${esc(t('Install {name}', { name: p.name }))}</button>`;
    } else if (!p.path && p.hint) {
      action = `<div class="muted mono" style="font-size:12px">${esc(p.hint)}</div>`;
    }
    if (ins.error && !ins.running) action += `<div class="bad-text" style="font-size:12.5px;margin-top:6px">${esc(ins.error)}</div>`;
    return `<div class="pcard ${sel ? 'active' : ''}" data-act="player-pick" data-id="${esc(p.id)}" role="radio" aria-checked="${!!sel}" tabindex="0">
      <div class="row"><b>${esc(p.name)}</b><span class="spacer"></span>${status}</div>
      <div class="muted pdesc">${esc(t(p.desc))}</div>
      ${p.path ? `<div class="mono ppath" title="${esc(p.path)}">${esc(p.path)}</div>` : ''}${action}</div>`;
  };
  const custom = kind === 'custom';
  const installing = r.players.find((p) => p.install && p.install.running);
  box.innerHTML = `<div class="pcards" role="radiogroup" aria-label="${esc(t('Player'))}">${r.players.map(card).join('')}
    <div class="pcard ${custom ? 'active' : ''}" data-act="player-pick" data-id="custom" role="radio" aria-checked="${custom}" tabindex="0">
      <div class="row"><b>${esc(t('Other player'))}</b><span class="spacer"></span>${custom && r.customFound ? `<span class="tag g">${esc(t('Found'))}</span>` : ''}</div>
      <div class="muted pdesc">${esc(t('Choose any program; it receives the path of the film.'))}</div>
      ${custom ? `<div class="row" style="margin-top:8px"><input id="m-path" value="${esc(plEdit.path || '')}" placeholder="${esc(t('Path of the program'))}"><button data-act="pick-file" data-target="m-path" aria-label="${esc(t('Browse'))}">${ICON.folder}</button></div>` : ''}</div></div>
    <div id="player-res" style="margin-top:10px">${installing ? `<span class="muted"><span class="spinner"></span> ${esc(t('Installing {name}…', { name: installing.name }))}</span>` :
      r.error ? `<span class="${kind ? 'bad-text' : 'warn-text'}">${esc(r.error)}</span>` : `<span class="ok-text">✓ ${esc(t('Films will open with {path}', { path: r.active.path }))}</span>`}</div>`;
  clearTimeout(playersTimer);
  if (installing) playersTimer = setTimeout(drawPlayers, 700);
}

async function installPlayer(id) {
  try {
    await api('POST', `/players/${encodeURIComponent(id)}/install`);
    plEdit.kind = id;
    if (state.settings?.player) state.settings.player.kind = id; // the server chooses it once installed
    drawPlayers();
    const wait = async () => {
      const r = await api('GET', '/players').catch(() => null);
      const p = r && r.players.find((x) => x.id === id);
      if (!p) return;
      if (p.install.running) { setTimeout(wait, 1000); return; }
      if (p.install.error) toast(p.install.error, 'bad');
      else { toast(t('{name} is installed', { name: p.name }), 'ok'); refreshStatus(); }
    };
    setTimeout(wait, 1000);
  } catch (e) { fail(e); }
}

/* ------------------------------------------------------- subtitle providers */
let SUBCAT = null; // catalog of providers
let subEdit = null; // subtitle settings being edited (settings or wizard draft)

async function loadSubCatalog() {
  if (!SUBCAT || SUBCAT.lang !== LANG) {
    SUBCAT = { lang: LANG, list: await api('GET', '/subtitles/providers') };
  }
  return SUBCAT.list;
}
const subInfo = (id) => (SUBCAT?.list || []).find((p) => p.id === id);
// providerScope tells whether a provider has films, series or both.
function providerScope(p) {
  const label = p.films && p.series ? t('Films and series') : p.series ? t('Series only') : t('Films only');
  return `<span class="tag ${p.series ? 'g' : ''}">${esc(label)}</span>`;
}
function subNeeds(p) {
  const req = (p.fields || []).filter((f) => f.required);
  if (!req.length) return (p.fields || []).length ? t('Account optional') : t('No account needed');
  return req.some((f) => f.type === 'password' && /key/i.test(f.key)) ? t('API key required') : t('Account required');
}

async function drawSubProviders() {
  const box = $('#sub-providers');
  if (!box) return;
  try { await loadSubCatalog(); } catch (e) { box.innerHTML = `<p class="bad-text">${esc(e.message)}</p>`; return; }
  const list = subEdit.providers || [];
  const paused = state.view === 'setup' ? {} : (await sourceStatus()).subtitles || {};
  box.innerHTML = `${list.length ? `<div class="list">${list.map((c, i) => {
    const p = subInfo(c.id) || { name: c.id, description: '' };
    const own = p.multiple && c.settings?.name;
    // OpenSubtitles.com without a key of the user's own uses the one of Cinearr
    const builtin = c.id === 'opensubtitles' && state.status?.openSubtitlesBuiltin && !(c.settings?.apiKey || '').trim();
    return `<div class="item">
      <input type="checkbox" data-act="sub-toggle" data-i="${i}" ${c.enabled ? 'checked' : ''} title="${esc(t('Enabled'))}">
      <div class="grow"><div class="name">${esc(own ? c.settings.name : p.name)} ${providerScope(p)}${builtin ? ` <span class="tag g">${esc(t('key provided by Cinearr'))}</span>` : ''}</div>
        <div class="det" title="${esc(p.description || '')}">${esc(builtin ? t('Edit it to enter your own key or your account.') : own ? (p.link ? c.settings.url : p.name) : p.description || '')}</div></div>
      ${pauseBadge(paused[own ? c.settings.name : p.name], 'subtitles', own ? c.settings.name : p.name)}
      <span id="sub-test-${i}" class="muted"></span>
      ${p.link ? '' : `<button class="small" data-act="sub-test" data-i="${i}">${esc(t('Test'))}</button>`}
      ${(p.fields || []).length ? `<button class="small" data-act="sub-edit" data-i="${i}">${esc(t('Edit'))}</button>` : ''}
      <button class="small ghost danger" data-act="sub-del" data-i="${i}" aria-label="${esc(t('Delete'))}">${ICON.trash}</button></div>`;
  }).join('')}</div>` : `<div class="callout warn">${esc(t('No provider yet: subtitles cannot be searched until you add one.'))}</div>`}
    <div class="row" style="margin-top:12px"><button class="primary" data-act="sub-add">${ICON.plus}${esc(t('Add a provider'))}</button></div>`;
}

async function persistSubtitles(msg) {
  if (state.view === 'setup') { drawSubProviders(); return; }
  try { await saveSettings(true); drawSubProviders(); if (msg) toast(msg, 'ok'); } catch (e) { fail(e); }
}

async function subAddModal() {
  await loadSubCatalog();
  const used = new Set((subEdit.providers || []).map((c) => c.id));
  const avail = SUBCAT.list.filter((p) => p.multiple || !used.has(p.id));
  const m = modal({ title: t('Add a subtitle provider'), wide: true,
    body: avail.length ? `<div class="pcards">${avail.map((p) => `<div class="pcard" data-pick-sub="${esc(p.id)}" tabindex="0" role="button">
      <div class="row"><b>${esc(p.name)}</b><span class="spacer"></span>${p.multiple ? '' : `<span class="tag ${(p.fields || []).some((f) => f.required) ? '' : 'g'}">${esc(subNeeds(p))}</span>`}</div>
      <div class="muted pdesc">${esc(p.description)}</div>
      <div class="muted" style="font-size:12px">${providerScope(p)}${p.multiple ? '' : ` ${esc(p.languages && p.languages.length ? t('Languages: {list}', { list: p.languages.join(', ').toUpperCase() }) : t('Many languages'))} · <span class="mono">${esc((p.url || '').replace(/^https?:\/\//, ''))}</span>`}</div></div>`).join('')}</div>` :
      `<p class="muted">${esc(t('Every available provider is already added.'))}</p>` });
  m.body.addEventListener('click', (e) => {
    const el = e.target.closest('[data-pick-sub]');
    if (!el) return;
    m.close();
    const p = subInfo(el.dataset.pickSub);
    const cfg = { id: p.id, enabled: true, settings: {} };
    if (!(p.fields || []).length) {
      subEdit.providers = [...(subEdit.providers || []), cfg];
      persistSubtitles(t('{name} added', { name: p.name }));
    } else subConfigModal(cfg, -1);
  });
  m.body.addEventListener('keydown', (e) => { if ((e.key === 'Enter' || e.key === ' ') && e.target.matches('[data-pick-sub]')) { e.preventDefault(); e.target.click(); } });
}

function subConfigModal(cfg, index) {
  const p = subInfo(cfg.id);
  const st = cfg.settings || {};
  const fields = (p.fields || []).map((f) => f.type === 'checkbox' ?
    `<label class="check"><input type="checkbox" data-sf="${esc(f.key)}" ${st[f.key] === 'true' ? 'checked' : ''}>${esc(f.label)}</label>${f.hint ? `<div class="hint muted" style="margin:-4px 0 10px 30px">${esc(f.hint)}</div>` : ''}` :
    `<div class="field"><label>${esc(f.label)}${f.required ? ' *' : ''}</label><input data-sf="${esc(f.key)}" type="${f.type === 'password' ? 'password' : 'text'}" value="${esc(st[f.key] || '')}" autocomplete="off">${f.hint ? `<div class="hint">${esc(f.hint)}</div>` : ''}</div>`).join('');
  const m = modal({ title: p.name,
    body: `<p class="muted" style="margin-top:0">${esc(p.description)}</p>
      ${p.signUp ? `<p><a href="${esc(p.signUp)}" target="_blank" rel="noopener">${esc(t('Create the account or the key on {site}', { site: p.signUp.replace(/^https?:\/\//, '').split('/')[0] }))}</a></p>` : ''}
      ${fields}`,
    footer: `<span id="sub-cfg-res" class="muted spacer"></span>${p.link ? '' : `<button data-test>${esc(t('Test'))}</button>`}<button class="primary" data-save>${esc(index < 0 ? t('Add') : t('Save'))}</button>` });
  const read = () => {
    const out = { ...cfg, settings: {} };
    $$('[data-sf]', m.body).forEach((el) => { out.settings[el.dataset.sf] = el.type === 'checkbox' ? String(el.checked) : el.value.trim(); });
    return out;
  };
  const res = $('#sub-cfg-res', m.el);
  if (!p.link) m.el.querySelector('[data-test]').onclick = async (e) => {
    const btn = e.currentTarget;
    btn.disabled = true;
    res.innerHTML = `<span class="spinner"></span> ${esc(t('Testing…'))}`;
    try { const r = await api('POST', '/subtitles/test', read()); res.innerHTML = `<span class="ok-text">✓ ${esc(r.message)}</span>`; }
    catch (err) { res.innerHTML = `<span class="bad-text">${esc(err.message)}</span>`; }
    btn.disabled = false;
  };
  m.el.querySelector('[data-save]').onclick = () => {
    const c = read();
    const missing = (p.fields || []).filter((f) => f.required && !c.settings[f.key]);
    if (missing.length) { res.innerHTML = `<span class="bad-text">${esc(t('Required: {list}', { list: missing.map((f) => f.label).join(', ') }))}</span>`; return; }
    const listp = [...(subEdit.providers || [])];
    if (index < 0) listp.push(c); else listp[index] = c;
    subEdit.providers = listp;
    m.close();
    const label = (p.multiple && c.settings.name) || p.name;
    persistSubtitles(index < 0 ? t('{name} added', { name: label }) : t('{name} saved', { name: label }));
  };
}

/* -------------------------------------------------------------------- setup */
// The motto of the author, shown on the welcome screen: kept in French in
// every language of the interface (also on the first page of the Windows
// installer, packaging/windows/cinearr.iss).
const MOTTO = '«\u00a0Pour que la culture soit accessible au plus grand nombre\u00a0»';
let setupStep = (() => { try { return +localStorage.getItem('cinearr.setupStep') || 0; } catch (e) { return 0; } })();
function rememberStep() { try { localStorage.setItem('cinearr.setupStep', String(setupStep)); } catch (e) { /* ignore */ } }
let draft = null;
// The TMDB step is only shown when Cinearr was built without its own TMDB
// key; otherwise a key of the user's own goes in Settings › General.
const SETUP_STEPS = () => ['welcome', ...(state.status?.tmdbBuiltin ? [] : ['tmdb']), 'client', 'library', 'player', 'indexers', 'subtitles'];

async function viewSetup(v) {
  if (!draft) draft = await api('GET', '/settings');
  await loadClients();
  const s = draft;
  editClients(s);
  plEdit = s.player;
  subEdit = s.subtitles;
  const steps = SETUP_STEPS();
  if (setupStep >= steps.length) setupStep = 0;
  const step = steps[setupStep];
  let body = '';
  switch (step) {
    case 'welcome':
      body = `<div class="choice lang-choice" role="radiogroup" aria-label="Language">${UI_LANGUAGES.map(([c, n]) => `<button type="button" role="radio" aria-checked="${c === LANG}" class="choice-item ${c === LANG ? 'active' : ''}" data-act="pick-lang" data-lang="${c}">${ICON.globe}${esc(n)}</button>`).join('')}</div>
        <h1>${esc(t('Welcome to Cinearr'))}</h1>
        <p class="motto" lang="fr">${esc(MOTTO)}</p>
        <p class="lead">${esc(t('Search a film or a series: Cinearr finds the best release on your sites, has your torrent client download it, stores it in your library, fetches the subtitles and opens it in your video player.'))}</p>
        <div class="callout">${esc(t('A few minutes of setup:'))}<ol>${state.status?.tmdbBuiltin ? '' : `<li>${esc(t('a free TMDB key (film information)'))}</li>`}<li>${esc(t('access to your torrent client (qBittorrent, Transmission, Deluge or µTorrent)'))}</li><li>${esc(t('the folders of your films and series'))}</li><li>${esc(t('the video player'))}</li><li>${esc(t('your sites (indexers)'))}</li><li>${esc(t('the subtitle providers (optional)'))}</li></ol></div>`;
      break;
    case 'tmdb':
      body = `<h1>${esc(t('Film database'))}</h1><p class="lead">${esc(t('TMDB provides the pages, posters and IMDb id of every film (used to find the right releases).'))}</p>
        ${state.status?.tmdbBuiltin ? `<div class="callout">${esc(t('Cinearr provides its own TMDB key: you can continue without entering one. If that key ever stops working, Cinearr will tell you, and you can then enter your own, free:'))}` : '<div class="callout">'}<ol><li>${t('Create an account on <a href="https://www.themoviedb.org/signup" target="_blank" rel="noopener">themoviedb.org</a>')}</li>
        <li>${esc(t('Settings › API › request a “Developer” key (personal use)'))}</li><li>${esc(t('Copy the “API Key” or the “API Read Access Token”'))}</li></ol></div>
        ${field(s, 'tmdbKey', state.status?.tmdbBuiltin ? t('Your TMDB key (optional)') : t('TMDB key'), '', 'password')}
        ${select(s, 'language', t('Language of the film pages'), LANGS)}
        <div class="row"><button data-act="w-test-tmdb">${esc(t('Check'))}</button><span id="w-res"></span></div>`;
      break;
    case 'client':
      body = `<h1>${esc(t('Torrent client'))}</h1><p class="lead">${esc(t('Choose the software that downloads your torrents: Cinearr drives it through its web interface.'))}</p>
        <div id="dc-form">${clientForm(dcEdit, false)}</div>`;
      break;
    case 'library':
      body = `<h1>${esc(t('Your library'))}</h1><p class="lead">${esc(t('The folders where finished films and series are stored.'))}</p>
        ${field(s, 'libraryPath', t('Films folder'), esc(t('Ideally on the same disk as the downloads of the torrent client (instant import with a hard link).')), 'text', `<button data-act="pick" data-target="${fieldId('libraryPath')}">${ICON.folder}${esc(t('Browse'))}</button>`)}
        ${field(s, 'seriesPath', t('Series folder'), esc(t('Optional now: needed before downloading episodes.')), 'text', `<button data-act="pick" data-target="${fieldId('seriesPath')}">${ICON.folder}${esc(t('Browse'))}</button>`)}`;
      break;
    case 'player':
      body = `<h1>${esc(t('Video player'))}</h1><p class="lead">${esc(t('Choose the player that will open your films. If it is not installed, Cinearr downloads it for you (portable version, stored in its data folder, without touching the rest of the system).'))}</p>
        <div id="player-box"><div class="loading"><span class="spinner"></span>${esc(t('Looking for installed players…'))}</div></div>`;
      break;
    case 'indexers':
      body = `<h1>${esc(t('Your sites'))}</h1><p class="lead">${esc(t('Indexers are the sites where films are searched. Hundreds of definitions (public and private sites) are offered, from the Prowlarr project.'))}</p>
        <div id="idx"></div><div class="row" style="margin-top:12px"><button class="primary" data-act="add-indexer">${ICON.plus}${esc(t('Add an indexer'))}</button></div>`;
      break;
    case 'subtitles':
      body = `<h1>${esc(t('Subtitles (optional)'))}</h1><p class="lead">${esc(t('Subtitles are searched automatically after each import, with Bazarr’s score. Choose the sites to use.'))}</p>
        ${field(s, 'subtitles.languages', t('Languages'), esc(t('ISO codes in order of preference: en, fr')), 'lower-list')}
        <div id="sub-providers"></div>`;
      break;
  }
  v.innerHTML = `<div class="setup"><div class="row"><img src="icon.svg" alt="" width="40"><b style="font-size:20px">Cinearr</b><span class="spacer"></span><span class="muted">${esc(t('Step {n} / {total}', { n: setupStep + 1, total: steps.length }))}</span></div>
    <div class="steps">${steps.map((_, i) => `<i class="${i <= setupStep ? 'done' : ''}"></i>`).join('')}</div>
    <div class="card" style="padding:28px" id="setup-body">${body}</div>
    <div class="row" style="margin-top:18px">${setupStep ? `<button data-act="w-prev">${esc(t('Back'))}</button>` : ''}<span class="spacer"></span>
      <button class="primary big" data-act="w-next">${esc(setupStep === steps.length - 1 ? t('Finish') : t('Continue'))}</button></div></div>`;
  if (step === 'player') drawPlayers();
  if (step === 'indexers') drawIndexers();
  if (step === 'subtitles') drawSubProviders();
}

function saveStep() {
  const body = $('#setup-body');
  if (!body || !draft) return;
  readBinds(draft, body);
  if ($('#dc-url')) readClientForm(draft.downloadClient);
  if ($('#player-box')) readPlayerForm(draft.player);
}

// pickLanguage switches the interface language at the first launch and
// adapts the defaults (film pages, preferred releases, subtitles, audio).
async function pickLanguage(code) {
  await setLanguage(code);
  draft.uiLanguage = code;
  try {
    const d = await api('GET', '/defaults?lang=' + code);
    draft.language = d.language;
    draft.profile = d.profile;
    draft.subtitles.languages = d.subtitleLanguages;
    draft.player.audioLang = d.audioLang;
  } catch (e) { /* keep the current values */ }
  SUBCAT = null;
  renderNav();
  viewSetup($('#view'));
}

async function setupNext() {
  saveStep();
  const steps = SETUP_STEPS();
  const step = steps[setupStep];
  if (step === 'tmdb' && !draft.tmdbKey && !state.status?.tmdbBuiltin) return toast(t('The TMDB key is needed to search films'), 'bad');
  if (step === 'library' && !draft.libraryPath) return toast(t('Choose a folder for your films'), 'bad');
  if (step === 'player' && playersInfo && playersInfo.error) toast(t('No player ready: you can install one later in Settings › Player'));
  if (!draft.uiLanguage) draft.uiLanguage = LANG;
  try {
    const cur = await api('GET', '/settings');
    draft.indexers = cur.indexers;
    if (setupStep === steps.length - 1) {
      draft.setupDone = true;
      await api('PUT', '/settings', draft);
      draft = null;
      setupStep = 0;
      rememberStep();
      await refreshStatus();
      toast(t('Setup complete. Enjoy your film!'), 'ok');
      location.hash = '#/discover';
      return;
    }
    await api('PUT', '/settings', draft);
    setupStep++;
    rememberStep();
    viewSetup($('#view'));
  } catch (e) { fail(e); }
}

/* ------------------------------------------------------------------ actions */
const actions = {
  quit: async () => {
    if (!(await confirmBox(t('Quit Cinearr'), te('Downloads continue in {client}; they will be imported at the next launch.', { client: clientName() }), t('Quit')))) return;
    try { await api('POST', '/quit'); } catch (e) { /* closing */ }
    document.body.innerHTML = `<div class="empty" style="padding-top:30vh"><h2>${esc(t('Cinearr is stopped'))}</h2><p>${esc(t('You can close this window.'))}</p></div>`;
    setTimeout(() => window.close(), 400);
  },
  'disc-more': () => loadMoreDiscover(),
  'disc-mode': (el) => { discoverMode = el.dataset.mode; $$('#disc-mode .chip').forEach((c) => c.classList.toggle('active', c === el)); if ($('#lookup')) $('#lookup').value = ''; loadDiscover(''); },
  'disc-kind': (el) => { if (discoverKind === el.dataset.kind) return; discoverKind = el.dataset.kind; viewDiscover($('#view')); },
  add: (el, e) => { e.stopPropagation(); if (el.dataset.kind === 'tv') addSeries(el.dataset.id, el); else addMovie(el.dataset.id, el); },
  preview: (el) => (el.dataset.kind === 'tv' ? previewSeries(el.dataset.id) : previewMovie(el.dataset.id)),
  'lib-filter': (el) => { libFilter = el.dataset.f; viewLibrary($('#view')); },
  'lib-kind': (el) => { libKind = el.dataset.k; libFilter = 'all'; history.replaceState(null, '', '#/library/' + libKind); viewLibrary($('#view')); },
  season: (el) => { seriesSeason[movieId()] = +el.dataset.n; seriesView?.draw(); },
  'ep-check': (el) => {
    const k = el.dataset.k;
    if (el.checked) seriesSel.add(k); else seriesSel.delete(k);
    seriesView?.draw();
    const again = document.querySelector(`[data-act="ep-check"][data-k="${k}"]`);
    if (again) again.focus();
  },
  'ep-all': (el) => {
    const s = seriesCache[movieId()];
    const se = (s?.seasons || []).find((x) => x.number === seriesSeason[movieId()]);
    (se?.episodes || []).filter((e) => aired(e) || e.file).forEach((e) => { const k = epKey(e.season, e.number); if (el.checked) seriesSel.add(k); else seriesSel.delete(k); });
    seriesView?.draw();
    $('[data-act="ep-all"]')?.focus();
  },
  'ep-none': () => { seriesSel.clear(); seriesView?.draw(); },
  'ep-open': (el) => {
    const k = el.dataset.k;
    if (episodeOpen.has(k)) episodeOpen.delete(k); else episodeOpen.add(k);
    seriesView?.draw();
  },
  'sub-import': (el) => importSubtitleModal(el.dataset.path),
  'ep-auto': async (el) => {
    const eps = selectedEpisodes();
    if (!eps.length || !(await needSeriesFolder())) return;
    el.disabled = true;
    try {
      await api('POST', `/series/${movieId()}/autosearch`, { episodes: eps });
      toast(plural(eps.length, 'Search started for {n} episode', 'Search started for {n} episodes'));
      seriesSel.clear();
      setTimeout(refreshStatus, 800);
    } catch (e) { fail(e); el.disabled = false; }
  },
  'ep-manual-sel': () => { const [k] = selectedEpisodes(); if (k) seriesManualSearch(movieId(), k.season, k.episode); },
  'ep-manual': (el) => seriesManualSearch(movieId(), +el.dataset.season, +el.dataset.episode),
  'season-manual': () => seriesManualSearch(movieId(), seriesSeason[movieId()], 0),
  'ep-grab': async (el) => {
    const d = lastDecisions[+el.dataset.i];
    if (!(await needSeriesFolder())) return;
    el.disabled = true;
    el.innerHTML = '<span class="spinner"></span>';
    try { await api('POST', `/series/${el.dataset.series}/grab`, { release: d.release, episodes: lastSeriesKeys }); toast(t('Sent to {client}', { client: clientName() }), 'ok'); $('#modal-root').innerHTML = ''; refreshStatus(); }
    catch (e) { fail(e); el.disabled = false; el.textContent = t('Retry'); }
  },
  'ep-play': async (el) => {
    // the list of the episode panel when it is open (the quick button of the row uses it too)
    const q = subQuery(`ep-sub-${el.dataset.season}-${el.dataset.episode}`);
    try { await api('POST', `/series/${movieId()}/play?season=${el.dataset.season}&episode=${el.dataset.episode}${q ? '&' + q : ''}`); toast(t('Playback started in the player')); } catch (e) { fail(e); }
  },
  'ep-play-live': async (el) => { try { await api('POST', `/series/${movieId()}/play?live=1&season=${el.dataset.season}&episode=${el.dataset.episode}`); toast(t('Playing the file being downloaded')); } catch (e) { fail(e); } },
  'ep-subs': (el) => subtitleModal(`/series/${movieId()}/subtitles?season=${el.dataset.season}&episode=${el.dataset.episode}`, t('Subtitles · {label}', { label: epLabel(+el.dataset.season, +el.dataset.episode) })),
  'sdl-cancel': (el) => {
    const s = state.series.find((x) => x.tmdbId === +el.dataset.series);
    const d = (s?.downloads || []).find((x) => x.hash === el.dataset.hash);
    cancelModal(d?.releaseTitle || '', `/series/${el.dataset.series}/cancel?hash=${encodeURIComponent(el.dataset.hash)}`);
  },
  'series-refresh': async () => { try { await api('POST', `/series/${movieId()}/refresh`); toast(t('Information updated')); seriesView?.load(); } catch (e) { fail(e); } },
  'series-folder': async () => { try { await api('POST', `/series/${movieId()}/open-folder`); } catch (e) { fail(e); } },
  'series-delete': () => {
    const id = movieId();
    const s = state.series.find((x) => x.tmdbId === id);
    deleteModal(t('Delete the series'), s?.title, s?.available ? t('Also delete the video files and the subtitles') : '', `/series/${id}`, t('Series deleted'), '#/library/series', () => { delete seriesCache[id]; });
  },
  'reset-app': () => resetModal(),
  scan: () => startScan(),
  'check-update': async () => {
    const out = $('#update-result');
    out.innerHTML = '<span class="spinner"></span>';
    try {
      const r = await api('GET', '/update');
      if (!r.newer) { out.innerHTML = `<span class="ok-text">✓ ${esc(t('Cinearr is up to date ({version}).', { version: r.current }))}</span>`; return; }
      out.innerHTML = `<span class="warn-text">${esc(t('Version {version} is available.', { version: r.latest }))}</span>
        ${r.installer ? `<button class="small primary" data-act="run-update">${esc(t('Update now'))}</button>` : (safeUrl(r.url) ? `<a class="btn small" href="${esc(safeUrl(r.url))}" target="_blank" rel="noopener">${esc(t('Download'))}</a>` : '')}`;
    } catch (e) { out.innerHTML = `<span class="bad-text">${esc(e.message)}</span>`; }
  },
  'run-update': async () => {
    try { await api('POST', '/update'); toast(t('The updater is starting: follow its instructions.'), 'ok'); } catch (e) { fail(e); }
  },
  'proxy-test': async () => {
    const out = $('#proxy-test');
    readTab();
    out.innerHTML = '<span class="spinner"></span>';
    try {
      const r = await api('POST', '/proxy/test', edit.proxy || {});
      const where = (x) => `${x.ip}${x.country ? ' (' + x.country + ')' : ''}`;
      out.innerHTML = r.same
        ? `<span class="warn-text">${esc(t('The proxy answers but the sites still see your own address {ip}', { ip: where(r.through) }))}</span>`
        : `<span class="ok-text">✓ ${esc(t('The sites see {ip}', { ip: where(r.through) }))}${r.direct?.ip ? ' · ' + esc(t('without the proxy: {ip}', { ip: where(r.direct) })) : ''}</span>`;
    } catch (e) { out.innerHTML = `<span class="bad-text">${esc(e.message)}</span>`; }
  },
  'resume-source': async (el) => {
    try {
      await api('POST', `/sources/resume?kind=${encodeURIComponent(el.dataset.kind)}&id=${encodeURIComponent(el.dataset.id)}`);
      if (el.dataset.kind === 'subtitles') drawSubProviders(); else drawIndexers();
    } catch (e) { fail(e); }
  },
  play: async () => {
    const q = subQuery('play-sub');
    try { await api('POST', `/movies/${movieId()}/play${q ? '?' + q : ''}`); toast(t('Playback started in the player')); } catch (e) { fail(e); }
  },
  'play-live': async () => { try { await api('POST', `/movies/${movieId()}/play?live=1`); toast(t('Playing the file being downloaded')); } catch (e) { fail(e); } },
  autosearch: async (el) => { el.disabled = true; try { await api('POST', `/movies/${movieId()}/autosearch`); toast(t('Search started')); setTimeout(refreshStatus, 800); } catch (e) { fail(e); el.disabled = false; } },
  manual: () => manualSearch(movieId()),
  grab: async (el) => {
    const d = lastDecisions[+el.dataset.i];
    el.disabled = true;
    el.innerHTML = '<span class="spinner"></span>';
    try { await api('POST', `/movies/${el.dataset.movie}/grab`, d.release); toast(t('Sent to {client}', { client: clientName() }), 'ok'); $('#modal-root').innerHTML = ''; refreshStatus(); }
    catch (e) { fail(e); el.disabled = false; el.textContent = t('Retry'); }
  },
  cancel: (el) => cancelDownload(el.dataset.movie || movieId()),
  'delete-movie': () => {
    const id = movieId();
    const m = state.movies.find((x) => x.tmdbId === id);
    deleteModal(t('Delete the film'), m?.title, m?.file ? t('Also delete the video file and the subtitles') : '', `/movies/${id}`, t('Film deleted'), '#/library/films');
  },
  'refresh-movie': async () => { try { await api('POST', `/movies/${movieId()}/refresh`); toast(t('Information updated')); refreshStatus(); } catch (e) { fail(e); } },
  'open-folder': async () => { try { await api('POST', `/movies/${movieId()}/open-folder`); } catch (e) { fail(e); } },
  subs: () => subtitleSearch(movieId()),
  'sub-dl': async (el) => {
    el.disabled = true;
    try { await api('POST', el.dataset.path, lastSubs[+el.dataset.i]); toast(t('Subtitles saved'), 'ok'); el.innerHTML = '✓'; refreshStatus(); seriesView?.load(); }
    catch (e) { fail(e); el.disabled = false; }
  },
  'add-indexer': () => addIndexerModal(),
  'idx-edit': (el) => { const c = (state.settings.indexers || []).find((x) => x.id === el.dataset.id); if (c) indexerConfigModal(c); },
  'idx-del': async (el) => {
    if (!(await confirmBox(t('Delete the indexer'), esc(t('This indexer will no longer be queried.')), t('Delete')))) return;
    try { await api('DELETE', '/indexers/' + encodeURIComponent(el.dataset.id)); drawIndexers(); refreshStatus(); } catch (e) { fail(e); }
  },
  'idx-test': async (el) => {
    const c = (state.settings.indexers || []).find((x) => x.id === el.dataset.id);
    const out = document.getElementById('test-' + el.dataset.id);
    out.innerHTML = '<span class="spinner"></span>';
    try { const r = await api('POST', '/indexers/test', c); out.innerHTML = `<span class="ok-text">✓ ${esc(plural(r.results, '{n} result', '{n} results'))}</span>`; el.closest('.item')?.querySelectorAll('[data-act=resume-source], .tag.warn').forEach((x) => x.remove()); }
    catch (e) { out.innerHTML = `<span class="bad-text" title="${esc(e.message)}">${esc(t('failed'))}</span>`; toast(e.message, 'bad'); }
  },
  'idx-toggle': async (el) => {
    const c = (state.settings.indexers || []).find((x) => x.id === el.dataset.id);
    if (!c) return;
    c.enabled = el.checked;
    try { await api('POST', '/indexers', c); } catch (e) { fail(e); }
  },
  'act-tab': (el) => { activityTab = el.dataset.t; viewActivity($('#view')); },
  'show-log': () => { activityTab = 'log'; if (location.hash === '#/activity') viewActivity($('#view')); else location.hash = '#/activity'; },
  'open-logs': async () => { try { await api('POST', '/logs/open'); } catch (e) { fail(e); } },
  unblock: async (el) => { try { await api('DELETE', '/blocklist/' + el.dataset.i); drawActivity(); } catch (e) { fail(e); } },
  'set-tab': (el) => {
    readTab();
    settingsTab = el.dataset.tab;
    $$('.tabs .tab').forEach((b) => { const on = b === el; b.classList.toggle('active', on); b.setAttribute('aria-selected', on); });
    history.replaceState(null, '', '#/settings/' + settingsTab);
    routeHash = location.hash;
    drawSettingsTab();
  },
  preset: (el) => {
    const p = RADARR_PROFILES[el.dataset.p];
    $$('[data-q]').forEach((c) => { c.checked = p.ids.includes(+c.dataset.q); });
    $('#' + fieldId('profile.cutoff')).value = String(p.cutoff);
    toast(t('Profile “{name}” applied: remember to save', { name: el.dataset.p }));
  },
  'save-settings': async (el) => {
    el.disabled = true;
    try { await saveSettings(false); hideUnsaved(); toast(t('Settings saved'), 'ok'); } catch (e) { fail(e); }
    el.disabled = false;
  },
  'test-client': async () => {
    const res = $('#dc-res');
    res.innerHTML = '<span class="spinner"></span>';
    let body;
    if (state.view === 'setup') { saveStep(); body = draft; } else body = readTab();
    try { const r = await api('POST', '/settings/test-client', body); res.innerHTML = `<span class="ok-text">✓ ${esc(r.name)} ${esc(r.version)}</span>${r.warning ? ` <span class="warn-text">${esc(r.warning)}</span>` : ''}`; }
    catch (e) { res.innerHTML = `<span class="bad-text">${esc(e.message)}</span>`; }
  },
  'dc-kind': (el) => switchClient(el.dataset.kind),
  'player-pick': (el, e) => {
    if (e.target.closest('button, input')) return;
    readPlayerForm(plEdit);
    plEdit.kind = el.dataset.id;
    drawPlayers();
  },
  'player-install': (el) => installPlayer(el.dataset.id),
  'sub-add': () => subAddModal(),
  'sub-edit': (el) => { const i = +el.dataset.i; subConfigModal(JSON.parse(JSON.stringify(subEdit.providers[i])), i); },
  'sub-del': async (el) => {
    const i = +el.dataset.i;
    const p = subInfo(subEdit.providers[i].id);
    if (!(await confirmBox(t('Remove the provider'), te('{name} will no longer be queried.', { name: p?.name || '' }), t('Remove')))) return;
    subEdit.providers = subEdit.providers.filter((_, j) => j !== i);
    persistSubtitles(t('{name} removed', { name: p?.name || '' }));
  },
  'sub-toggle': (el) => { subEdit.providers[+el.dataset.i].enabled = el.checked; persistSubtitles(); },
  'sub-test': async (el) => {
    const i = +el.dataset.i;
    const out = $('#sub-test-' + i);
    out.innerHTML = '<span class="spinner"></span>';
    try { const r = await api('POST', '/subtitles/test', subEdit.providers[i]); out.innerHTML = `<span class="ok-text">✓ ${esc(r.message)}</span>`; }
    catch (e) { out.innerHTML = `<span class="bad-text">${esc(e.message)}</span>`; }
  },
  pick: async (el) => { const p = await pick(true, t('Choose a folder')); if (p) { $('#' + el.dataset.target).value = p; } },
  'pick-file': async (el) => { const p = await pick(false, t('Choose the player')); if (p) { $('#' + el.dataset.target).value = p; drawPlayers(); } },
  'pick-lang': (el) => pickLanguage(el.dataset.lang),
  'w-next': () => setupNext(),
  'w-prev': () => { saveStep(); setupStep = Math.max(0, setupStep - 1); rememberStep(); viewSetup($('#view')); },
  'w-test-tmdb': async () => {
    const res = $('#w-res');
    res.innerHTML = '<span class="spinner"></span>';
    try {
      const r = await api('POST', '/settings/test-tmdb', { key: $('#' + fieldId('tmdbKey')).value.trim(), language: $('#' + fieldId('language')).value });
      res.innerHTML = `<span class="ok-text">✓ ${esc(r.builtin ? t('The key provided with Cinearr works') : t('Valid key'))}</span>`;
    }
    catch (e) { res.innerHTML = `<span class="bad-text">${esc(e.message)}</span>`; }
  },
};

function movieId() { return +(location.hash.split('/')[2] || 0); }

document.addEventListener('click', (e) => {
  const el = e.target.closest('[data-act]');
  if (!el || !actions[el.dataset.act]) return;
  if (el.tagName !== 'INPUT') e.preventDefault();
  actions[el.dataset.act](el, e);
});

document.addEventListener('keydown', (e) => {
  if ((e.key === 'Enter' || e.key === ' ') && e.target.matches('[role="radio"][data-act]')) { e.preventDefault(); e.target.click(); }
});

window.addEventListener('hashchange', route);

(async function init() {
  try {
    state.status = await api('GET', '/status');
    state.movies = await api('GET', '/movies');
    state.series = await api('GET', '/series');
    state.settings = await api('GET', '/settings');
  } catch (e) { /* first paint without data */ }
  let lang = state.settings?.uiLanguage;
  if (!lang) { try { lang = localStorage.getItem('cinearr.lang'); } catch (e) { /* ignore */ } }
  await setLanguage(lang || 'en');
  route();
  checkCrash();
  setInterval(refreshStatus, 3000);
})();
