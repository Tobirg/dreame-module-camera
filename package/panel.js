// Camera panel, mounted by the Dreame Local web UI: mount(element, ctx).
export default function mount(el, ctx) {
  el.innerHTML = `<label class="check"><input data-r="enabled" type="checkbox">Показывать снимки камеры</label>
<p class="muted small">Во время уборки прошивка сама фотографирует фронтальной камерой и складывает несколько последних снимков на робот (для сбора данных ИИ). Модуль только показывает их здесь: снимки не покидают робот через модуль. Выключено — модуль снимки не отдаёт.</p>
<div class="row"><button data-r="live">Смотреть вживую</button></div>
<p data-r="live-status" class="muted small" role="status"></p>
<img data-r="live-img" alt="Камера робота вживую" hidden style="max-width:100%;border-radius:8px">
<p class="muted small">Живой просмотр работает, пока робот на базе и не спит, и пока открыта эта панель. На роботе в это время горит индикатор камеры.</p>
<p data-r="status" class="muted small" role="status"></p>
<div class="row"><select data-r="list" aria-label="Снимок"></select><button data-r="refresh">Обновить</button></div>
<img data-r="photo" alt="Снимок камеры робота" hidden style="max-width:100%;border-radius:8px;margin-top:8px">`;
  const $ = name => el.querySelector(`[data-r="${name}"]`);
  const time = s => new Date(s * 1000).toLocaleString();
  let active = false, shown = '', last = 0;

  async function show(name) {
    try {
      const p = await ctx.call('photo', name ? { name } : {});
      $('photo').src = 'data:image/jpeg;base64,' + p.jpeg; $('photo').hidden = false; shown = p.name;
    } catch (e) { $('photo').hidden = true; $('status').textContent = e.message; }
  }
  function render(s) {
    $('enabled').checked = s.enabled;
    $('list').innerHTML = s.photos.map(p => `<option value="${p.name}">${time(p.at)}</option>`).join('');
    $('list').disabled = $('refresh').disabled = $('live').disabled = !s.enabled;
    if (!s.enabled) stopWatch();
    $('status').textContent = !s.folder ? 'Папки со снимками ещё нет: она появится после первой уборки.'
      : s.photos.length ? 'Снимков: ' + s.photos.length + ', последний ' + time(s.photos[0].at) : 'Снимков нет: прошивка удаляет их после отправки или новой уборки.';
    if (!s.enabled) { $('photo').hidden = true; shown = ''; return; }
    if (s.photos.length && s.photos[0].name !== shown && (!shown || $('list').selectedIndex <= 0)) show(s.photos[0].name);
  }
  async function refresh() { try { render(await ctx.read('status')); } catch (e) { $('status').textContent = e.message; } }

  // Live view: the handler keeps the camera-live service capturing while it is asked for frames.
  let watching = false;
  async function watch() {
    while (watching && !document.hidden && el.isConnected) {
      try {
        const f = await ctx.call('live');
        if (f.jpeg) { $('live-img').src = 'data:image/jpeg;base64,' + f.jpeg; $('live-img').hidden = false; $('live-status').textContent = 'Вживую · ' + time(f.at); }
        else $('live-status').textContent = f.reason || (f.state === 'live' || f.state === 'starting' || f.state === 'off' ? 'Включаю камеру…' : f.state);
      } catch (e) { $('live-status').textContent = e.message; stopWatch(); }
      await new Promise(r => setTimeout(r, 700));
    }
    stopWatch();
  }
  function stopWatch() { watching = false; $('live').textContent = 'Смотреть вживую'; $('live-img').hidden = true; }
  $('live').onclick = () => { if (watching) { stopWatch(); return; } watching = true; $('live').textContent = 'Остановить'; watch(); };
  $('enabled').onchange = async () => {
    try { render(await ctx.call('set', { enabled: $('enabled').checked })); } catch (e) { ctx.toast(e.message, true); refresh(); }
  };
  $('list').onchange = () => show($('list').value);
  $('refresh').onclick = refresh;
  // New photos appear only while the robot cleans: follow them then.
  ctx.onState(s => {
    const cleaning = s.activity === 'cleaning';
    if (cleaning && !document.hidden && Date.now() - last > 5000) { last = Date.now(); refresh(); }
    else if (active && !cleaning) refresh();
    active = cleaning;
  });
  refresh();
}
