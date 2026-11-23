// Camera panel, mounted by the Dreame Local web UI: mount(element, ctx).
export default function mount(el, ctx) {
  el.innerHTML = `<label class="check"><input data-r="enabled" type="checkbox">Показывать снимки камеры</label>
<p class="muted small">Во время уборки прошивка сама фотографирует фронтальной камерой и складывает несколько последних снимков на робот (для сбора данных ИИ). Модуль только показывает их здесь: снимки не покидают робот через модуль. Выключено — модуль снимки не отдаёт.</p>
<div class="row"><button data-r="live">Смотреть вживую</button><label class="check"><input data-r="light" type="checkbox">Подсветка при просмотре</label></div>
<p data-r="live-status" class="muted small" role="status"></p>
<img data-r="live-img" alt="Камера робота вживую" hidden>
<div data-r="pad">
<span></span><button data-move="200,0" aria-label="Вперёд">▲</button><span></span>
<button data-move="0,60" aria-label="Влево">◀</button><button data-r="dock" aria-label="На базу">⌂</button><button data-move="0,-60" aria-label="Вправо">▶</button>
<span></span><button data-move="-150,0" aria-label="Назад">▼</button><span></span></div>
<p class="muted small">Изображение идёт, пока открыта эта панель, в том числе во время уборки; на роботе горит индикатор камеры. «Подсветка при просмотре» включает фронтальный фонарь робота, пока камера снимает (здесь или в Home Assistant). Кнопки движения (или стрелки на клавиатуре) — ручное управление: робот едет, пока кнопка нажата; первое нажатие сводит его с базы, через 15 с без команд он останавливается. ⌂ — на базу.</p>
<p data-r="status" class="muted small" role="status"></p>
<div class="row"><select data-r="list" aria-label="Снимок"></select><button data-r="refresh">Обновить</button></div>
<img data-r="photo" alt="Снимок камеры робота" hidden>
<details data-r="code-box"><summary>Код доступа к видео в приложении Dreame</summary>
<p data-r="code-status" class="muted small" role="status"></p>
<form data-r="code-form"><label data-r="code-old-label">Текущий код<input data-r="code-old" type="password" inputmode="numeric" pattern="[0-9]{4}" maxlength="4" autocomplete="off"></label>
<label>Новый код<input data-r="code-new" type="password" inputmode="numeric" pattern="[0-9]{4}" maxlength="4" autocomplete="off" required></label><button type="submit">Задать код</button></form>
<button data-r="code-reset">Сбросить код кнопкой «Домой»</button>
<p class="muted small">Код из 4 цифр нужен только облачному видеонаблюдению в приложении Dreame; просмотр здесь и управление роботом его не спрашивают. Забыл код — нажми «Сбросить» и в течение 5 минут нажми «Домой» на роботе три раза подряд: робот удалит код и скажет об этом.</p></details>`;
  const $ = name => el.querySelector(`[data-r="${name}"]`);
  // The agent's CSP drops style attributes in panel HTML; styles set from script apply.
  Object.assign($('pad').style, { display: 'none', maxWidth: '240px', gridTemplateColumns: 'repeat(3, 1fr)', gap: '6px', marginTop: '8px' });
  for (const name of ['photo', 'live-img']) Object.assign($(name).style, { maxWidth: '100%', borderRadius: '8px', marginTop: '8px' });
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
    $('light').checked = s.light;
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
        // Not ctx.call: that one blocks the other controls of the page while it runs, and this polls.
        const f = await ctx.rpc('/api/v1/m/camera/live', {});
        if (f.jpeg) { $('live-img').src = 'data:image/jpeg;base64,' + f.jpeg; $('live-img').hidden = false; $('live-status').textContent = 'Вживую · ' + time(f.at); }
        else $('live-status').textContent = f.reason || (f.state === 'live' || f.state === 'starting' || f.state === 'off' ? 'Включаю камеру…' : f.state);
      } catch (e) { $('live-status').textContent = e.message; stopWatch(); }
      await new Promise(r => setTimeout(r, 400));
    }
    stopWatch();
  }
  function stopWatch() { watching = false; drive(null); $('live').textContent = 'Смотреть вживую'; $('live-img').hidden = true; $('pad').style.display = 'none'; }
  $('live').onclick = () => { if (watching) { stopWatch(); return; } watching = true; $('pad').style.display = 'grid'; $('live').textContent = 'Остановить'; watch(); };

  // Manual drive: one step every 250 ms while a direction is held (the robot moves a short step per command).
  let driving = null, driveTimer = 0;
  const step = () => ctx.rpc('/api/v1/command', { name: 'remote_move', velocity: driving[0], rotation: driving[1] })
    .catch(e => { ctx.toast(e.message, true); drive(null); });
  function drive(move) {
    clearInterval(driveTimer); driving = move;
    if (move) { step(); driveTimer = setInterval(step, 250); }
  }
  for (const b of el.querySelectorAll('[data-move]')) {
    const move = b.dataset.move.split(',').map(Number);
    b.onpointerdown = e => { e.preventDefault(); b.setPointerCapture(e.pointerId); drive(move); };
    b.onpointerup = b.onpointercancel = () => drive(null);
  }
  const keys = { ArrowUp: '200,0', ArrowDown: '-150,0', ArrowLeft: '0,60', ArrowRight: '0,-60' };
  document.addEventListener('keydown', e => {
    if (!watching || !keys[e.key] || e.repeat || e.target.closest?.('input,select,textarea')) return;
    e.preventDefault(); drive(keys[e.key].split(',').map(Number));
  });
  document.addEventListener('keyup', e => { if (keys[e.key] && driving) drive(null); });
  $('dock').onclick = () => ctx.rpc('/api/v1/command', { name: 'dock' }).then(() => ctx.toast('Робот едет на базу'), e => ctx.toast(e.message, true));
  $('enabled').onchange = async () => {
    try { render(await ctx.call('set', { enabled: $('enabled').checked })); } catch (e) { ctx.toast(e.message, true); refresh(); }
  };
  $('light').onchange = async () => {
    try { render(await ctx.call('light', { on: $('light').checked })); } catch (e) { ctx.toast(e.message, true); refresh(); }
  };
  // Video access code: while allowed, three presses of Home on the robot clear it.
  let codeWaiting = false, codePoll = 0;
  async function code(params) {
    clearTimeout(codePoll);
    const c = await ctx.call(params.code ? 'code_set' : 'code', params);
    codeWaiting = c.waiting;
    $('code-old-label').hidden = !c.code_set; $('code-old').required = c.code_set;
    $('code-status').textContent = c.waiting ? 'Жду: нажми «Домой» на роботе три раза подряд.' : c.reset ? 'Код сброшен кнопкой «Домой».' : c.code_set ? 'Код задан.' : 'Код не задан.';
    $('code-reset').textContent = c.waiting ? 'Отменить' : 'Сбросить код кнопкой «Домой»';
    if (c.reset) ctx.toast('Код доступа к видео сброшен');
    if (c.waiting && el.isConnected) codePoll = setTimeout(() => code({}).catch(e => ctx.toast(e.message, true)), 3000);
  }
  $('code-box').ontoggle = () => { if ($('code-box').open) code({}).catch(e => { $('code-status').textContent = e.message; }); };
  $('code-reset').onclick = () => code({ enabled: !codeWaiting }).catch(e => ctx.toast(e.message, true));
  $('code-form').onsubmit = e => {
    e.preventDefault();
    const old = $('code-old'), neu = $('code-new');
    code({ code: neu.value, ...(old.required ? { old_code: old.value } : {}) })
      .then(() => { old.value = neu.value = ''; ctx.toast('Код доступа к видео задан'); }, e => ctx.toast(e.message, true));
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
