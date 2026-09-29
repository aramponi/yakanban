// The hero clip: a Claude Code session in a terminal beside the board it
// drives, replayed from a timeline. Nothing in here knows what the session
// was; it only knows how to show one.
//
// A timeline is JSON:
//
//   {
//     "recorded": "2026-09-29",     when the session was captured
//     "version":  "v0.9.0",         the yakanban that captured it
//     "real":     284000,           ms the session actually took
//     "duration": 57000,            ms the clip takes, before the loop restarts
//     "title":    "claude — acme-api",
//     "events": [ { "t": ms, "type": ..., ... } ]
//   }
//
// Event types:
//
//   prompt  {text}                      the user types a prompt
//   say     {text}                      the assistant says something
//   tool    {cmd, out: [lines]}         a command and, shortly after, its output
//   board   {name, columns: [names]}    the board appears
//   card    {id, title, column, priority}
//   move    {id, column, agent?}        a card changes column; agent claims it
//   agent   {id, task, label, model, effort}   a sub-agent starts
//   agent   {id, activity}                     what it is doing now
//   agent   {id, status: "done"|"running", note?}   it stops, or is resumed
//
// internal/site/demo builds a timeline from a recorded session.
//
// The page renders the final frame, without motion, for readers who asked the
// system for reduced motion.
(function () {
  'use strict';

  var TYPE_MS = 32;      // per character of a typed prompt
  var OUT_DELAY = 420;   // between a command and its output
  var HOLD_MS = 3500;    // final frame, before the loop restarts
  var FADE_MS = 450;
  var MOVE_MS = 520;

  function h(tag, cls, text) {
    var n = document.createElement(tag);
    if (cls) n.className = cls;
    if (text != null) n.textContent = text;
    return n;
  }

  function Player(root, tl) {
    this.root = root;
    this.tl = tl;
    // Output is its own event so that it lands after the command, not with it.
    var evs = [];
    tl.events.forEach(function (e) {
      evs.push(e);
      if (e.type === 'tool' && e.out && e.out.length) {
        evs.push({ t: e.t + OUT_DELAY, type: 'out', lines: e.out });
      }
    });
    this.events = evs.sort(function (a, b) { return a.t - b.t; });
    this.duration = Math.max(tl.duration || 0, evs.length ? evs[evs.length - 1].t + 1000 : 0);

    this.userPaused = false;
    this.visible = false;
    this.last = 0;
    this.build();
    this.reset();
  }

  Player.prototype.build = function () {
    var r = this.root;
    r.textContent = '';
    r.classList.add('demo');

    var stage = h('div', 'demo-stage');
    stage.setAttribute('aria-hidden', 'true');

    var term = h('div', 'demo-term');
    var tbar = h('div', 'demo-bar');
    tbar.appendChild(h('span', 'demo-dots'));
    tbar.appendChild(h('span', 'demo-bar-title', this.tl.title || 'claude'));
    term.appendChild(tbar);
    var screen = h('div', 'demo-screen');
    this.log = h('div', 'demo-log');
    screen.appendChild(this.log);
    term.appendChild(screen);

    var board = h('div', 'demo-board');
    var bbar = h('div', 'demo-bar');
    this.boardName = h('span', 'demo-bar-title', '');
    bbar.appendChild(h('span', 'demo-project-icon'));
    bbar.appendChild(this.boardName);
    bbar.appendChild(h('span', 'demo-bar-tab', 'Board'));
    board.appendChild(bbar);
    this.cols = h('div', 'demo-cols');
    board.appendChild(this.cols);

    stage.appendChild(term);
    stage.appendChild(board);
    r.appendChild(stage);
    this.stage = stage;

    var cap = h('div', 'demo-caption');
    this.btn = h('button', 'demo-pause', 'Pause');
    this.btn.type = 'button';
    var self = this;
    this.btn.addEventListener('click', function () {
      self.userPaused = !self.userPaused;
      self.btn.textContent = self.userPaused ? 'Play' : 'Pause';
      self.btn.setAttribute('aria-pressed', self.userPaused ? 'true' : 'false');
    });
    this.btn.setAttribute('aria-pressed', 'false');
    cap.appendChild(this.btn);
    cap.appendChild(h('span', 'demo-note', this.caption()));
    r.appendChild(cap);

    // The stage is decoration for sighted readers; this is what everyone
    // else gets.
    r.setAttribute('role', 'group');
    r.setAttribute('aria-label', 'Recording: Claude Code sets up a yakanban board, files tickets, ' +
      'and hands each ticket to a sub-agent with its own model and effort, while the board updates beside it.');
  };

  function span(ms) {
    var s = Math.round(ms / 1000), m = Math.floor(s / 60);
    return m ? m + ' min ' + (s % 60 ? (s % 60) + ' s' : '') : s + ' s';
  }

  Player.prototype.caption = function () {
    var tl = this.tl;
    if (tl.provisional) return 'Provisional timeline, for developing the player only.';
    var s = 'A real session, recorded ' + tl.recorded;
    if (tl.version) s += ' with yakanban ' + tl.version;
    if (tl.real && tl.duration) s += ': ' + span(tl.real) + ' of work, shown in ' + span(tl.duration);
    return s + '.';
  };

  Player.prototype.reset = function () {
    this.now = 0;
    this.next = 0;
    this.typing = null;
    this.agents = {};
    this.cards = {};
    this.columns = {};
    this.log.textContent = '';
    this.cols.textContent = '';
    this.cols.appendChild(h('div', 'demo-empty', 'No board yet'));
    this.boardName.textContent = '';
    this.stage.style.opacity = '';
  };

  // ---------- terminal ----------

  Player.prototype.line = function (cls) {
    var l = h('div', 'demo-line ' + (cls || ''));
    this.log.appendChild(l);
    return l;
  };

  Player.prototype.stopTyping = function () {
    if (!this.typing) return;
    this.typing.span.textContent = this.typing.text;
    this.typing.cursor.remove();
    this.typing = null;
  };

  // ---------- board ----------

  Player.prototype.column = function (name) {
    var c = this.columns[name];
    if (!c) throw new Error('demo: no column "' + name + '"');
    return c;
  };

  Player.prototype.counts = function () {
    for (var k in this.columns) {
      var c = this.columns[k];
      c.count.textContent = c.cards.children.length;
    }
  };

  // Moves a card, and lets every card the move displaces glide rather than
  // jump: record where they were, change the DOM, animate from there.
  Player.prototype.flip = function (change, animate) {
    var before = [];
    if (animate) {
      for (var id in this.cards) {
        before.push([this.cards[id].node, this.cards[id].node.getBoundingClientRect()]);
      }
    }
    change();
    this.counts();
    if (!animate) return;
    before.forEach(function (p) {
      var n = p[0], a = p[1], b = n.getBoundingClientRect();
      var dx = a.left - b.left, dy = a.top - b.top;
      if (!dx && !dy) return;
      n.animate([
        { transform: 'translate(' + dx + 'px,' + dy + 'px)' },
        { transform: 'none' }
      ], { duration: MOVE_MS, easing: 'cubic-bezier(.2,.75,.25,1)' });
    });
  };

  // Model and effort are separate spans so a narrow card can stack them.
  Player.prototype.chip = function (agent) {
    var c = h('span', 'demo-chip demo-m-' + String(agent.model).toLowerCase());
    c.appendChild(h('span', 'demo-chip-model', agent.model));
    if (agent.effort) c.appendChild(h('span', 'demo-chip-effort', agent.effort));
    return c;
  };

  // ---------- events ----------

  Player.prototype.apply = function (e, animate) {
    var self = this, l, a, c;
    if (e.type !== 'out') this.stopTyping();

    switch (e.type) {
      case 'prompt':
        l = this.line('demo-prompt');
        l.appendChild(h('span', 'demo-caret', '>'));
        var span = h('span', 'demo-typed', '');
        var cursor = h('span', 'demo-cursor');
        l.appendChild(span);
        l.appendChild(cursor);
        this.typing = { span: span, cursor: cursor, text: e.text, t: e.t };
        if (!animate) this.stopTyping();
        break;

      case 'say':
        l = this.line('demo-say');
        l.appendChild(h('span', 'demo-bullet'));
        l.appendChild(h('span', '', e.text));
        break;

      case 'tool':
        l = this.line('demo-tool');
        l.appendChild(h('span', 'demo-bullet'));
        l.appendChild(h('span', 'demo-tool-name', 'Bash'));
        l.appendChild(h('span', 'demo-cmd', e.cmd));
        break;

      case 'out':
        e.lines.forEach(function (text, i) {
          var o = self.line('demo-out');
          o.appendChild(h('span', 'demo-elbow', i === 0 ? '⎿' : ''));
          o.appendChild(h('span', '', text));
        });
        break;

      case 'agent':
        a = this.agents[e.id];
        if (e.activity) {
          if (!a.line.classList.contains('is-done')) a.status.textContent = e.activity;
          break;
        }
        if (e.status) {
          var done = e.status === 'done';
          a.line.classList.toggle('is-done', done);
          a.status.textContent = e.status;
          if (e.note) a.status.appendChild(h('span', 'demo-status-note', ' · ' + e.note));
          break;
        }
        l = this.line('demo-agent');
        l.appendChild(h('span', 'demo-bullet'));
        l.appendChild(h('span', 'demo-tool-name', 'Agent'));
        l.appendChild(h('span', 'demo-agent-task', '#' + e.task));
        l.appendChild(h('span', 'demo-agent-label', e.label));
        l.appendChild(this.chip(e));
        var st = h('span', 'demo-status', 'running');
        l.appendChild(st);
        this.agents[e.id] = { line: l, status: st, model: e.model, effort: e.effort };
        break;

      case 'board':
        this.boardName.textContent = e.name;
        this.cols.textContent = '';
        e.columns.forEach(function (name, i) {
          var col = h('div', 'demo-col');
          var head = h('div', 'demo-col-head');
          head.appendChild(h('span', 'demo-col-dot demo-col-' + i));
          head.appendChild(h('span', 'demo-col-name', name));
          var count = h('span', 'demo-count', '0');
          head.appendChild(count);
          var cards = h('div', 'demo-cards');
          col.appendChild(head);
          col.appendChild(cards);
          self.cols.appendChild(col);
          self.columns[name] = { cards: cards, count: count, last: i === e.columns.length - 1 };
        });
        if (animate) this.cols.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 400 });
        break;

      case 'card':
        c = h('div', 'demo-card demo-p-' + (e.priority || 'none'));
        var top = h('div', 'demo-card-top');
        top.appendChild(h('span', 'demo-card-id', '#' + e.id));
        if (e.priority) top.appendChild(h('span', 'demo-prio', e.priority));
        c.appendChild(top);
        c.appendChild(h('div', 'demo-card-title', e.title));
        var claim = h('div', 'demo-card-claim');
        c.appendChild(claim);
        this.cards[e.id] = { node: c, claim: claim };
        this.flip(function () { self.column(e.column).cards.appendChild(c); }, animate);
        if (animate) {
          c.animate([
            { opacity: 0, transform: 'translateY(-6px) scale(.97)' },
            { opacity: 1, transform: 'none' }
          ], { duration: 360, easing: 'ease-out' });
        }
        break;

      case 'move':
        c = this.cards[e.id];
        var to = this.column(e.column);
        if (e.agent) {
          c.claim.textContent = '';
          c.claim.appendChild(this.chip(this.agents[e.agent]));
        }
        c.node.classList.toggle('is-done', to.last);
        this.flip(function () { to.cards.appendChild(c.node); }, animate);
        break;
    }
  };

  // ---------- clock ----------

  Player.prototype.advance = function (animate) {
    while (this.next < this.events.length && this.events[this.next].t <= this.now) {
      this.apply(this.events[this.next++], animate);
    }
    if (this.typing) {
      var n = Math.floor((this.now - this.typing.t) / TYPE_MS);
      this.typing.span.textContent = this.typing.text.slice(0, Math.max(0, n));
    }
  };

  Player.prototype.tick = function (ts) {
    var dt = this.last ? Math.min(ts - this.last, 100) : 0;
    this.last = ts;
    if (!this.userPaused && this.visible && !this.fading) {
      this.now += dt;
      this.advance(true);
      if (this.now >= this.duration + HOLD_MS) {
        var self = this;
        this.fading = true;
        this.stage.animate([{ opacity: 1 }, { opacity: 0 }], { duration: FADE_MS, fill: 'forwards' })
          .onfinish = function (ev) {
            ev.target.cancel();
            self.reset();
            self.stage.animate([{ opacity: 0 }, { opacity: 1 }], { duration: FADE_MS });
            self.fading = false;
          };
      }
    }
    var p = this;
    requestAnimationFrame(function (t) { p.tick(t); });
  };

  Player.prototype.play = function () {
    var self = this;
    this.onscreen = false;
    var sync = function () { self.visible = self.onscreen && !document.hidden; };
    if ('IntersectionObserver' in window) {
      new IntersectionObserver(function (entries) {
        self.onscreen = entries[0].isIntersecting;
        sync();
      }, { threshold: 0.35 }).observe(this.root);
    } else {
      this.onscreen = true;
      sync();
    }
    document.addEventListener('visibilitychange', sync);
    requestAnimationFrame(function (t) { self.tick(t); });
  };

  Player.prototype.still = function () {
    this.now = this.duration;
    this.advance(false);
    this.btn.hidden = true;
  };

  function mount(root, timeline) {
    var p = new Player(root, timeline);
    if (matchMedia('(prefers-reduced-motion: reduce)').matches) p.still();
    else p.play();
    return p;
  }

  window.YakanbanDemo = { mount: mount };

  // The page embeds the timeline next to the element that shows it.
  document.querySelectorAll('[data-demo]').forEach(function (root) {
    var src = document.getElementById(root.getAttribute('data-demo'));
    if (src) mount(root, JSON.parse(src.textContent));
  });
})();
