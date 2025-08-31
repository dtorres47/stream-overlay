// grab elements
const msgs = document.getElementById("messages");
const questList = document.getElementById("quests");
const requestsList = document.getElementById("requests");
const audioGateBtn = document.getElementById("audioGate");
const wsStatus = document.getElementById("wsStatus");

// state
const cooldown = new Map();
const questElems = new Map();
const requestElems = new Map();
const audCache = new Map();
let audioEnabled = false;

// audio gate
function enableAudio() {
    const a = new Audio("data:audio/mp3;base64,//uQZ...");
    a.play().catch(()=>{}).finally(() => {
        audioEnabled = true;
        audioGateBtn.style.display = "none";
    });
}
audioGateBtn.addEventListener("click", enableAudio);

// helpers
function toast(text) {
    if (!text) return;
    const d = document.createElement("div");
    d.className = "toast";
    d.textContent = text;
    msgs.appendChild(d);
}
function beep(ms=200, freq=880) {
    try {
        const C = new (window.AudioContext||window.webkitAudioContext)();
        const o = C.createOscillator(), g = C.createGain();
        o.connect(g); g.connect(C.destination);
        o.type = "square"; o.frequency.value = freq;
        g.gain.setValueAtTime(0.2, C.currentTime);
        o.start(); o.stop(C.currentTime + ms/1000);
    } catch {}
}
function clamp01(x) {
    x = Number(x);
    if (!isFinite(x)) return undefined;
    return Math.max(0, Math.min(1, x));
}
function playAbility(id, url, volume) {
    const cached = audCache.get(id);
    const vol = clamp01(volume);
    if (cached && cached.audio) {
        if (url !== cached.url) {
            const na = new Audio(url);
            na.preload = "auto";
            na.volume = vol ?? cached.volume ?? 0.7;
            const entry = { audio: na, ready: false, url, volume: na.volume };
            na.addEventListener("canplaythrough", () => entry.ready = true, { once: true });
            na.load();
            audCache.set(id, entry);
            try { na.currentTime = 0; na.play().catch(()=>{}); } catch {}
            return;
        }
        try {
            cached.audio.volume = vol ?? cached.volume ?? cached.audio.volume ?? 0.7;
            cached.audio.currentTime = 0;
            cached.audio.play().catch(()=>{});
            return;
        } catch {}
    }
    try {
        const a = new Audio(url);
        a.volume = vol ?? 0.7;
        a.play().catch(()=>{});
    } catch { beep(); }
}

function speak(text, voiceHint) {
    if (!("speechSynthesis" in window)) { toast("TTS not supported"); return; }
    if (!text) return;
    const u = new SpeechSynthesisUtterance(text);
    if (voiceHint) {
        const voices = speechSynthesis.getVoices();
        const match = voices.find(v => v.name.toLowerCase().includes(voiceHint.toLowerCase()));
        if (match) u.voice = match;
    }
    u.rate = 1; u.pitch = 1; u.volume = 1;
    speechSynthesis.speak(u);
}
if ('speechSynthesis' in window) {
    speechSynthesis.getVoices();
    if (speechSynthesis.onvoiceschanged !== undefined)
        speechSynthesis.onvoiceschanged = () => {};
}

// quests
function renderQuest(d) {
    const id = d.id; if (!id) return;
    let el = questElems.get(id);
    const progress = Number(d.progress||0),
        target   = Math.max(1, Number(d.target||1));
    const label = `${d.name||"Quest"} — ${progress}/${target}`;
    const html = d.icon_url
        ? `<span style="display:inline-flex;align-items:center;gap:8px;">
         <img src="${d.icon_url}" style="width:20px;height:20px;" />
         <span>${label}</span>
       </span>`
        : `🗡️ ${label}`;
    if (!el) {
        el = document.createElement("div");
        el.className = "quest"; el.dataset.id = id;
        questList.appendChild(el);
        questElems.set(id, el);
    }
    el.innerHTML = html;
    const done = progress>=target;
    el.style.opacity = done ? "0.75" : "1";
    el.style.textDecoration = done ? "line-through" : "none";
}
function removeQuest(id) {
    const el = questElems.get(id);
    if (el) { el.remove(); questElems.delete(id); }
}

// requests
function renderRequest(d) {
    const id = d.id; if (!id) return;
    let el = requestElems.get(id);
    const label = `${d.board||"(request)"}${d.note?" — "+d.note:""}`;
    const phone = d.masked_phone?` <small style="color:#9f9">(${d.masked_phone})</small>`:"";
    const html = `📞 ${label}${phone}`;
    if (!el) {
        el = document.createElement("div");
        el.className = "request"; el.dataset.id = id;
        requestsList.appendChild(el);
        requestElems.set(id, el);
    }
    el.innerHTML = html;
}
function removeRequest(id) {
    const el = requestElems.get(id);
    if (el) { el.remove(); requestElems.delete(id); }
}

// preload ability sounds
async function preloadSounds() {
    try {
        const data = await fetch('/api/catalog').then(r=>r.json());
        const list = data.abilities||[];
        let count=0;
        list.forEach(a=>{
            if (!a.id||!a.sfx_url) return;
            const entry = {
                audio: new Audio(a.sfx_url),
                ready: false,
                url: a.sfx_url,
                volume: clamp01(a.volume)||0.7
            };
            entry.audio.preload="auto";
            entry.audio.volume=entry.volume;
            entry.audio.addEventListener("canplaythrough",()=>entry.ready=true,{once:true});
            entry.audio.load();
            audCache.set(a.id, entry);
            count++;
        });
        if (count) toast(`Preloading ${count} sound${count===1?"":"s"}…`);
    } catch {}
}
window.addEventListener('load', preloadSounds);

// WebSocket w/ auto-reconnect
const wsUrl = (location.protocol==="https:"?"wss://":"ws://")+location.host+"/ws";
let ws, retry=0;
function connectWS() {
    wsStatus.textContent="WS: connecting";
    ws = new WebSocket(wsUrl);
    ws.onopen = ()=>{
        retry=0; wsStatus.textContent="WS: connected";
        toast("WebSocket open → "+wsUrl);
    };
    ws.onmessage = ev=>{
        let msg;
        try { msg = JSON.parse(ev.data) } catch { return }
        if (!msg||!msg.type) return;
        const d = msg.data||{};
        switch(msg.type) {
            case "DONATION":
                const cents=Number(d.amount||0),
                    dollars=isFinite(cents)?(cents/100).toFixed(2):"0.00";
                toast(`💸 ${d.donor||"Anonymous"} donated $${dollars}${d.msg?" — "+d.msg:""}`);
                break;
            case "ABILITY_FIRE":
                const id=d.id||"ability",
                    cd=Number(d.cooldown_ms||3000),
                    now=Date.now(),
                    until=cooldown.get(id)||0;
                if(now>=until){
                    cooldown.set(id,now+(isFinite(cd)?cd:3000));
                    d.sfx_url?playAbility(id,d.sfx_url,d.volume):beep();
                }
                break;
            case "TTS_PLAY":
                speak(d.text,d.voice);
                break;
            case "QUEST_UPSERT": renderQuest(d); break;
            case "QUEST_ADD": d.progress=d.progress||0; d.target=d.target||1; renderQuest(d); break;
            case "QUEST_REMOVE": if(d.id) removeQuest(d.id); break;
            case "REQUEST_ADD": renderRequest(d); break;
            case "REQUEST_REMOVE": if(d.id) removeRequest(d.id); break;
        }
    };
    ws.onclose = ()=>{
        wsStatus.textContent="WS: disconnected";
        const delay = Math.min(30000,1000*Math.pow(2,retry++));
        wsStatus.textContent=`WS: reconnecting in ${Math.round(delay/1000)}s`;
        setTimeout(connectWS,delay);
    };
    ws.onerror = ()=>{};
}
connectWS();