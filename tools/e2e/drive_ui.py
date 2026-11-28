# Drives the interface of tools/e2e with Playwright (pip install playwright).
# Run it through tools/e2e/run.sh, or start the harness first:
#   PATH=$PWD/tools/e2e/fakebin:$PATH go run ./tools/e2e > /tmp/e2e.log
# (tools/e2e/fakebin/vlc is a fake player that records its arguments).
import json, os, sys, time
from playwright.sync_api import sync_playwright

info = json.loads(open('/tmp/e2e.log').readline().split(' ', 1)[1])
BASE = 'http://127.0.0.1:7980/'
shots = os.environ.get('E2E_SHOTS', 'e2e-shots/')
os.makedirs(shots, exist_ok=True)
errors = []

def wait_text(pg, sel, text, timeout=10):
    for _ in range(int(timeout / 0.1)):
        if text in pg.inner_text(sel):
            return
        time.sleep(0.1)
    raise AssertionError(f'{text!r} not in {sel}: {pg.inner_text(sel)[:200]!r}')

with sync_playwright() as p:
    b = p.chromium.launch()
    pg = b.new_page(viewport={'width': 1440, 'height': 900})
    pg.on('console', lambda m: errors.append(m.text) if m.type == 'error' else None)
    pg.on('pageerror', lambda e: errors.append(str(e)))
    pg.goto(BASE)
    pg.wait_for_selector('.setup')
    wait_text(pg, '.setup', 'Welcome to Cinearr')  # English by default
    pg.screenshot(path=shots + '01-setup-welcome-en.png')
    pg.click('[data-act=pick-lang][data-lang=fr]')
    wait_text(pg, '.setup', 'Bienvenue dans Cinearr')
    # the motto stays in French in every language
    assert 'Pour que la culture soit accessible au plus grand nombre' in pg.inner_text('.setup .motto')
    assert pg.evaluate("getComputedStyle(document.querySelector('.setup .motto')).fontStyle") == 'italic'
    pg.screenshot(path=shots + '01-setup-welcome-fr.png')
    # the built-in TMDB key: no TMDB step, the torrent client comes next
    pg.click('[data-act=w-next]')
    pg.wait_for_selector('[data-act=dc-kind]')
    assert not pg.query_selector('#f-tmdbKey')
    wait_text(pg, '.setup', 'Étape 2 / 6')
    pg.click('[data-act=dc-kind][data-kind=transmission]')
    assert pg.input_value('#dc-url') == 'http://127.0.0.1:9091', pg.input_value('#dc-url')
    pg.fill('#dc-url', info['transmission']); pg.fill('#dc-user', 'admin'); pg.fill('#dc-pass', 'secret')
    pg.click('[data-act=test-client]'); pg.wait_for_selector('#dc-res .ok-text')
    pg.screenshot(path=shots + '03-setup-client.png')
    pg.click('[data-act=w-next]')
    pg.fill('#f-libraryPath', info['library'])
    pg.fill('#f-seriesPath', info['series'])
    pg.click('[data-act=w-next]')
    pg.wait_for_selector('.pcard')
    lang = pg.evaluate("fetch('/api/settings').then(r => r.json()).then(s => s.language)")
    assert lang == 'fr-FR', lang  # the film pages follow the language chosen first
    pg.click('.pcard[data-id=vlc]'); pg.wait_for_selector('#player-res .ok-text')
    pg.screenshot(path=shots + '04-setup-player.png')
    pg.click('[data-act=w-next]')
    # indexers step: add the demo tracker
    pg.click('.setup [data-act=add-indexer]')
    pg.wait_for_selector('#def-list .def')
    assert pg.locator('[data-act=pick-torznab]').count() == 1
    pg.fill('#def-q', 'demo')
    pg.screenshot(path=shots + '05-indexer-list.png')
    pg.wait_for_timeout(500); pg.click('#def-list [data-act=pick-def]')
    pg.wait_for_selector('[data-test]')
    pg.click('[data-test]'); pg.wait_for_selector('#cfg-result .ok-text', timeout=30000)
    pg.screenshot(path=shots + '06-indexer-config.png')
    pg.click('[data-save]')
    pg.wait_for_selector('#idx .item')
    pg.click('[data-act=w-next]')
    # subtitles step: choose a provider without account
    pg.fill('#f-subtitles-languages', 'fr, en')
    pg.click('[data-act=sub-add]')
    pg.wait_for_selector('[data-pick-sub=yifysubtitles]')
    pg.screenshot(path=shots + '07-subtitle-catalog.png')
    pg.click('[data-pick-sub=yifysubtitles]')
    pg.wait_for_selector('#sub-providers .item')
    pg.screenshot(path=shots + '08-setup-subtitles.png')
    pg.click('[data-act=w-next]')
    pg.wait_for_selector('#disc .poster')
    time.sleep(0.5)
    assert 'Doomsday' not in pg.inner_text('#disc'), 'unreleased film listed'
    pg.screenshot(path=shots + '09-discover.png')
    pg.click('#disc .poster[data-id="27205"]')
    pg.wait_for_selector('.preview-poster')
    time.sleep(0.4)
    box = pg.locator('.preview-poster').bounding_box()
    assert abs(box['height'] / box['width'] - 1.5) < 0.05, box
    pg.screenshot(path=shots + '10-preview.png')
    pg.click('[data-addnow]')
    # the user stays on Discover to add other titles; the poster says it is added
    pg.wait_for_selector('#disc .poster[data-id="27205"] .badge.lib')
    assert pg.evaluate('location.hash') == '#/discover', pg.evaluate('location.hash')
    assert not pg.query_selector('.modal'), 'preview still open'
    pg.click('#disc .poster[data-id="27205"]')
    pg.wait_for_selector('.hero h1')
    time.sleep(1)
    # adding does not download anything by itself
    assert pg.locator('.hero .badge.wanted').count() == 1, pg.inner_text('.hero')
    pg.screenshot(path=shots + '11-movie-added.png')
    pg.click('.hero [data-act=autosearch]')
    pg.wait_for_selector('.hero .badge.downloading, .hero .badge.wanted:has-text("%"), .hero .badge.importing, .hero .badge.available', timeout=60000)
    pg.goto(BASE + '#/downloads'); time.sleep(1.5)
    if pg.locator('#dl [data-act=cancel]').count():
        pg.click('#dl [data-act=cancel]')
        pg.wait_for_selector('#cancel-block')
        assert not pg.is_checked('#cancel-block')
        pg.screenshot(path=shots + '12-cancel-dialog.png')
        pg.click('.modal [data-no]')
    pg.goto(BASE + '#/movie/27205')
    pg.wait_for_selector('.hero .badge.available', timeout=90000)
    time.sleep(1)
    pg.screenshot(path=shots + '13-movie-available.png')
    pg.click('.hero [data-act=manual]')
    pg.wait_for_selector('.modal table', timeout=60000)
    assert 'Shutter' not in pg.inner_text('.modal'), 'release of another film shown'
    assert pg.locator('.modal button:has-text("Télécharger")').count() > 0, 'download button label'
    pg.screenshot(path=shots + '14-manual-search.png')
    pg.keyboard.press('Escape')
    pg.click('[data-act=play]'); time.sleep(1)
    pg.goto(BASE + '#/settings'); pg.wait_for_selector('.tabs'); time.sleep(.5)
    pg.screenshot(path=shots + '15-settings-general.png')
    for tab in ['client', 'quality', 'subtitles', 'player']:
        pg.click(f'[data-act=set-tab][data-tab={tab}]'); time.sleep(.6)
        pg.screenshot(path=shots + f'16-settings-{tab}.png')
    # switching tabs keeps unsaved values
    pg.click('[data-act=set-tab][data-tab=general]')
    pg.fill('#f-tmdbKey', 'changed-key')
    pg.click('[data-act=set-tab][data-tab=library]')
    pg.click('[data-act=set-tab][data-tab=general]')
    assert pg.input_value('#f-tmdbKey') == 'changed-key'
    # leaving with changes not saved: the page stays, the box shakes at each try
    pg.click('#nav a[href="#/discover"]'); pg.wait_for_selector('#unsaved')
    assert pg.evaluate('location.hash').startswith('#/settings'), pg.evaluate('location.hash')
    pg.screenshot(path=shots + '16b-settings-unsaved.png')
    pg.click('#nav a[href="#/library"]'); time.sleep(.1)
    assert 'shake' in pg.get_attribute('#unsaved', 'class')
    pg.click('#unsaved [data-unsaved=discard]')
    pg.wait_for_selector('#lib')  # it goes where the user wanted to go
    assert not pg.query_selector('#unsaved')
    pg.goto(BASE + '#/settings/general'); pg.wait_for_selector('#f-tmdbKey')
    assert pg.input_value('#f-tmdbKey') != 'changed-key'
    # "Save" in the box saves, then leaves
    pg.fill('#f-tmdbKey', 'demo-key')
    pg.click('#nav a[href="#/discover"]'); pg.wait_for_selector('#unsaved')
    pg.click('#unsaved [data-unsaved=save]')
    pg.wait_for_selector('#disc')
    assert pg.evaluate("fetch('/api/settings').then(r => r.json()).then(s => s.tmdbKey)") == 'demo-key'
    # without changes, every tab visited, leaving is free
    pg.goto(BASE + '#/settings/general'); pg.wait_for_selector('#f-tmdbKey')
    for tab in ['client', 'library', 'quality', 'subtitles', 'player', 'data', 'general']:
        pg.click(f'[data-act=set-tab][data-tab={tab}]'); time.sleep(.3)
    pg.click('#nav a[href="#/discover"]'); pg.wait_for_selector('#disc')
    assert not pg.query_selector('#unsaved'), 'settings seen as changed without any change'
    pg.goto(BASE + '#/settings/general'); pg.wait_for_selector('#f-tmdbKey')
    pg.fill('#f-tmdbKey', 'demo-key')
    # change the interface language from the settings
    pg.select_option('#f-uiLanguage', 'de')
    pg.click('.page-head [data-act=save-settings]')
    wait_text(pg, '.tabs', 'Allgemein')
    pg.screenshot(path=shots + '17-settings-de.png')
    pg.select_option('#f-uiLanguage', 'fr')
    pg.click('.page-head [data-act=save-settings]')
    wait_text(pg, '.tabs', 'Général')
    # proxy (VPN): the address typed with its port is split on save
    pg.click('[data-act=set-tab][data-tab=general]')
    pg.select_option('#f-proxy-type', 'socks5')
    pg.fill('#f-proxy-host', 'socks5://nl.socks.example.com:1080')
    pg.click('.page-head [data-act=save-settings]'); time.sleep(.8)
    pg.goto(BASE + '#/settings/general'); pg.wait_for_selector('#f-proxy-host'); time.sleep(.3)
    assert pg.input_value('#f-proxy-host') == 'nl.socks.example.com', pg.input_value('#f-proxy-host')
    assert pg.input_value('#f-proxy-port') == '1080'
    pg.locator('#f-proxy-host').scroll_into_view_if_needed(); time.sleep(.2)
    pg.screenshot(path=shots + '15-settings-proxy.png')
    pg.select_option('#f-proxy-type', '')
    pg.click('.page-head [data-act=save-settings]'); time.sleep(.5)
    # import checks: extensions typed freely are cleaned on save
    pg.click('[data-act=set-tab][data-tab=library]')
    pg.fill('#f-rejectedExtensions', 'URL, *.iso')
    pg.fill('#f-minFreeSpaceMB', '200')
    pg.click('.page-head [data-act=save-settings]'); time.sleep(.8)
    pg.goto(BASE + '#/settings/library'); pg.wait_for_selector('#f-rejectedExtensions'); time.sleep(.3)
    assert pg.input_value('#f-rejectedExtensions') == '.url, .iso', pg.input_value('#f-rejectedExtensions')
    assert pg.input_value('#f-minFreeSpaceMB') == '200'
    pg.locator('#f-rejectedExtensions').scroll_into_view_if_needed(); time.sleep(.2)
    pg.screenshot(path=shots + '16-settings-library.png')
    pg.fill('#f-rejectedExtensions', '')
    pg.click('.page-head [data-act=save-settings]'); time.sleep(.5)
    pg.goto(BASE + '#/activity'); pg.wait_for_selector('#act table'); time.sleep(.3)
    pg.screenshot(path=shots + '18-history.png')

    # ---- series
    pg.goto(BASE + '#/discover'); pg.wait_for_selector('#disc .poster')
    pg.click('[data-act=disc-kind][data-kind=tv]')
    pg.wait_for_selector('#disc .poster[data-kind=tv]')
    time.sleep(.5)
    assert 'Future Show' not in pg.inner_text('#disc'), 'unaired series listed'
    pg.screenshot(path=shots + '20-discover-series.png')
    pg.fill('#lookup', 'twin')
    pg.wait_for_selector('#disc-title:has-text("twin")')
    pg.click('#disc .poster[data-id="1920"]')
    pg.wait_for_selector('.preview-poster')
    time.sleep(.3)
    pg.screenshot(path=shots + '21-preview-series.png')
    pg.click('[data-addnow]')
    pg.wait_for_selector('#disc .poster[data-id="1920"] .badge.lib')
    assert pg.evaluate('location.hash').startswith('#/discover'), pg.evaluate('location.hash')
    pg.click('#disc .poster[data-id="1920"]')
    pg.wait_for_selector('table.eps')
    time.sleep(.6)
    pg.screenshot(path=shots + '22-series-page.png')
    pg.click('[data-act=season][data-n="2"]')
    for e in (1, 2, 3):
        pg.click(f'[data-act=ep-check][data-k="2:{e}"]')
    wait_text(pg, '.ep-toolbar', '3')
    assert pg.is_disabled('[data-act=ep-manual-sel]'), 'manual search must need a single episode'
    pg.screenshot(path=shots + '23-series-selection.png')
    pg.click('[data-act=ep-auto]')
    for _ in range(120):
        txt = pg.inner_text('table.eps')
        if txt.count('Disponible') >= 3:
            break
        time.sleep(1)
    else:
        raise AssertionError('episodes not imported: ' + pg.inner_text('table.eps')[:400])
    time.sleep(.5)
    pg.screenshot(path=shots + '24-series-available.png')
    pg.click('[data-act=ep-manual][data-season="2"][data-episode="1"]')
    pg.wait_for_selector('.modal table', timeout=60000)
    pg.screenshot(path=shots + '25-episode-manual.png')
    pg.keyboard.press('Escape')
    pg.click('[data-act=season-manual]')
    pg.wait_for_selector('.modal table', timeout=60000)
    assert 'S02' in pg.inner_text('.modal')
    pg.screenshot(path=shots + '26-season-manual.png')
    pg.keyboard.press('Escape')
    pg.click('[data-act=ep-play][data-season="2"][data-episode="1"]'); time.sleep(1)
    # unfolded episode: cast, subtitles, own site, file import
    pg.click('[data-act=ep-open][data-k="2:1"]')
    pg.wait_for_selector('.ep-detail .person')
    assert 'Kyle MacLachlan' in pg.inner_text('.ep-detail')
    pg.screenshot(path=shots + '26b-episode-panel.png')
    pg.click('.ep-detail [data-act=sub-import]')
    pg.wait_for_selector('#imp-file')
    srt = '/tmp/cinearr-e2e-sub.srt'
    open(srt, 'w').write('1\n00:00:01,000 --> 00:00:02,000\nBonjour\n')
    pg.set_input_files('#imp-file', srt)
    pg.select_option('#imp-lang', 'fr')
    pg.click('.modal [data-yes]')
    wait_text(pg, '.ep-detail', 'ajouté depuis un fichier', 15)
    pg.screenshot(path=shots + '26c-subtitle-imported.png')
    # subtitle chosen before the launch: the French file, then none
    log = os.environ.get('CINEARR_PLAYER_LOG', '/tmp/cinearr-player-args.txt')
    pg.wait_for_selector('#ep-sub-2-1')
    assert pg.input_value('#ep-sub-2-1') == '0', pg.input_value('#ep-sub-2-1')  # French first (preferred)
    pg.click('.ep-detail [data-act=ep-play]'); time.sleep(1)
    played = open(log).read()
    # the chosen file is shown first, VLC keeps the others of the folder
    assert '.fr.srt' in played and '--sub-file=' in played and '--no-sub-autodetect-file' not in played, played
    pg.select_option('#ep-sub-2-1', 'none')
    time.sleep(3.5)  # a periodic redraw keeps the choice
    assert pg.input_value('#ep-sub-2-1') == 'none', 'choice lost on redraw'
    pg.click('.ep-detail [data-act=ep-play]'); time.sleep(1)
    played = open(log).read()
    assert '--sub-language=none' in played and '--sub-file=' not in played, played
    pg.screenshot(path=shots + '26d-subtitle-choice.png')
    # the page does not jump back up while the user reads the episodes
    pg.evaluate("document.getElementById('view').scrollTop = document.getElementById('view').scrollHeight")
    bottom = pg.evaluate("document.getElementById('view').scrollTop")
    assert bottom > 100, bottom
    time.sleep(4)
    assert pg.evaluate("document.getElementById('view').scrollTop") == bottom, 'scroll moved by a redraw'
    pg.goto(BASE + '#/library/series'); pg.wait_for_selector('#lib .poster'); time.sleep(.3)
    pg.screenshot(path=shots + '27-library-series.png')
    pg.click('[data-act=lib-kind][data-k=films]'); pg.wait_for_selector('#lib .poster')
    pg.screenshot(path=shots + '28-library-films.png')
    pg.click('#scan-btn'); wait_text(pg, '#toasts', 'Bibliothèque mise à jour', 20)
    pg.wait_for_timeout(300); pg.screenshot(path=shots + '29-library-scan.png')

    # ---- own subtitle site: offered in the subtitle search of an episode
    pg.goto(BASE + '#/settings/subtitles'); pg.wait_for_selector('#sub-providers .item')
    pg.click('[data-act=sub-add]'); pg.wait_for_selector('[data-pick-sub=gestdown]')
    pg.screenshot(path=shots + '27b-subtitle-catalog.png')
    pg.click('[data-pick-sub=custom-link]'); pg.wait_for_selector('[data-sf=url]')
    pg.fill('[data-sf=name]', 'MesSousTitres'); pg.fill('[data-sf=url]', 'https://subs.example.org/search?q={query}')
    pg.click('.modal [data-save]'); wait_text(pg, '#sub-providers', 'MesSousTitres')
    pg.goto(BASE + '#/series/1920'); pg.wait_for_selector('table.eps')
    pg.click('[data-act=season][data-n="2"]')
    pg.click('.ep-acts [data-act=ep-subs][data-season="2"][data-episode="1"]')
    pg.wait_for_selector('.sub-extra a.btn')
    href = pg.get_attribute('.sub-extra a.btn', 'href')
    assert href == 'https://subs.example.org/search?q=Twin+Peaks+S02E01', href
    pg.screenshot(path=shots + '27c-subtitle-sites.png')
    pg.keyboard.press('Escape')

    # ---- reset empties the library lists, never the files
    files_before = [os.path.join(d, f) for d, _, fs in os.walk(info['library']) for f in fs]
    assert files_before, 'no film file before the reset'
    pg.goto(BASE + '#/settings/data'); pg.wait_for_selector('[data-act=reset-app]')
    pg.screenshot(path=shots + '29-settings-data.png')
    pg.click('[data-act=reset-app]'); pg.wait_for_selector('#reset-players')
    assert not pg.query_selector('#reset-lib')
    pg.screenshot(path=shots + '30-reset-dialog.png')
    pg.click('.modal [data-yes]')
    pg.wait_for_selector('.setup')
    wait_text(pg, '.setup', 'Welcome to Cinearr')
    lib = pg.evaluate("fetch('/api/movies').then(r => r.json())")
    ser = pg.evaluate("fetch('/api/series').then(r => r.json())")
    assert len(lib) == 0 and len(ser) == 0, (len(lib), len(ser))
    assert all(os.path.exists(f) for f in files_before), 'a file was touched by the reset'
    scan = pg.evaluate("fetch('/api/library/scan').then(r => r.json())")
    assert not scan.get('finished') or scan['finished'].startswith('0001'), scan  # old report forgotten
    pg.screenshot(path=shots + '31-after-reset.png')
    # the wizard again, with the same folders: when it ends, the folders are
    # read and the library is filled from the files on disk
    for _ in range(3):  # welcome, client, library
        if pg.query_selector('#f-libraryPath'):
            break
        pg.click('[data-act=w-next]'); time.sleep(0.4)
    pg.fill('#f-libraryPath', info['library']); pg.fill('#f-seriesPath', info['series'])
    while pg.query_selector('[data-act=w-next]'):
        pg.click('[data-act=w-next]'); time.sleep(0.4)
        if pg.query_selector('.pcard[data-id=vlc]') and not pg.query_selector('.pcard[data-id=vlc].active'):
            pg.click('.pcard[data-id=vlc]'); pg.wait_for_selector('#player-res .ok-text')
    for _ in range(100):
        lib = pg.evaluate("fetch('/api/movies').then(r => r.json())")
        if lib:
            break
        time.sleep(0.2)
    assert [m['title'] for m in lib] == ['Inception'], [m['title'] for m in lib]
    pg.goto(BASE + '#/library'); time.sleep(1)
    assert 'filmtest' not in pg.inner_text('#view')
    pg.screenshot(path=shots + '32-library-after-new-setup.png')
    b.close()
bad = [e for e in errors if 'image.tmdb.org' not in e and 'ERR_' not in e]
assert not bad, f'console errors: {bad}'
args = os.environ.get('CINEARR_PLAYER_LOG', '/tmp/cinearr-player-args.txt')
print('player args:', open(args).read().split('\n') if os.path.exists(args) else None)
print('INTERFACE OK')
