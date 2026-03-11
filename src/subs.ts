// Subtitle waterfall + OpenSubtitles picker UI

export async function initSubtitles(player: any, video: any) {
    // 1. Check what exists
    const info = await fetch(`/subs/info?file=${encodeURIComponent(video.raw)}`)
        .then(r => r.json())
        .catch(() => null);
    if (!info) return;

    if (info.vtt) {
        // Already loaded via the track element, nothing to do
        return;
    }

    if (info.srt) {
        // Backend will auto-convert on serve — just reload the track
        reloadTrack(player, `/subs/${video.sub}`);
        return;
    }

    // Auto-extract first English embedded track
    if (info.embedded?.length > 0) {
        const eng = info.embedded.find((t: any) => t.language === 'en' || t.language === 'eng') ?? info.embedded[0];
        await fetch('/subs/extract', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ file: video.raw, track: eng.index })
        });
        reloadTrack(player, `/subs/${video.sub}`);
        return;
    }

    // Auto-search OpenSubtitles
    await searchAndShowPicker(player, video);
}

async function searchAndShowPicker(player: any, video: any, force = false) {
    const results = await fetch(`/subs/search?file=${encodeURIComponent(video.raw)}`)
        .then(r => r.json())
        .catch(() => ({ results: [] }));

    if (results.error === 'no_api_key') {
        if (force) showWhisperButton(player, video);
        return;
    }

    if (!results.results?.length) {
        showWhisperButton(player, video);
        return;
    }

    showSubsPicker(player, video, results.results);
}

function reloadTrack(player: any, src: string) {
    const tracks = player.remoteTextTracks();
    for (let i = tracks.length - 1; i >= 0; i--) {
        player.removeRemoteTextTrack(tracks[i]);
    }
    player.addRemoteTextTrack(
        { kind: 'captions', src, srclang: 'en', label: 'English', default: true },
        false
    );
}

function showSubsPicker(player: any, video: any, results: any[]) {
    removePicker();
    const modal = document.createElement('div');
    modal.id = 'subs-picker';
    modal.innerHTML = `
        <div class="subs-backdrop"></div>
        <div class="subs-modal">
            <h3>Select Subtitles</h3>
            <ul class="subs-list">
                ${results.map(r => `
                    <li data-file-id="${r.file_id}">
                        ${r.hash_match ? '<span class="subs-match">✓ exact match</span>' : ''}
                        <span class="subs-release">${r.release}</span>
                        <span class="subs-dl">${r.download_count.toLocaleString()} downloads</span>
                    </li>
                `).join('')}
            </ul>
            <button class="subs-whisper-btn">Generate with Whisper instead</button>
            <button class="subs-close">✕</button>
        </div>
    `;
    document.body.appendChild(modal);

    modal.querySelector('.subs-backdrop')!.addEventListener('click', removePicker);
    modal.querySelector('.subs-close')!.addEventListener('click', removePicker);
    modal.querySelectorAll('.subs-list li').forEach(li => {
        li.addEventListener('click', async () => {
            const fileId = parseInt((li as HTMLElement).dataset.fileId!);
            li.textContent = 'Downloading...';
            const res = await fetch('/subs/download', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ file_id: fileId, file: video.raw })
            }).then(r => r.json()).catch(() => ({ ok: false }));
            if (res.ok) {
                removePicker();
                reloadTrack(player, `/subs/${video.sub}`);
            } else {
                li.textContent = 'Failed — try another';
            }
        });
    });
    modal.querySelector('.subs-whisper-btn')!.addEventListener('click', () => {
        removePicker();
        startWhisper(player, video);
    });
}

function showWhisperButton(player: any, video: any) {
    removeWhisperBtn();
    const btn = document.createElement('button');
    btn.id = 'whisper-btn';
    btn.textContent = 'Generate subtitles with Whisper';
    btn.className = 'whisper-gen-btn';
    btn.addEventListener('click', () => startWhisper(player, video));
    document.querySelector('.title')!.insertAdjacentElement('afterend', btn);
}

async function startWhisper(player: any, video: any) {
    removeWhisperBtn();
    const status = document.createElement('div');
    status.id = 'whisper-status';
    status.textContent = '⏳ Generating subtitles with Whisper (this takes a while)…';
    document.querySelector('.title')!.insertAdjacentElement('afterend', status);

    await fetch('/subs/whisper', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ file: video.raw })
    });

    const poll = setInterval(async () => {
        const s = await fetch(`/subs/whisper/status?file=${encodeURIComponent(video.raw)}`)
            .then(r => r.json())
            .catch(() => null);
        if (!s) return;
        if (s.status === 'done') {
            clearInterval(poll);
            status.remove();
            reloadTrack(player, `/subs/${video.sub}`);
        } else if (s.status === 'error') {
            clearInterval(poll);
            status.textContent = '✗ Whisper failed: ' + (s.error || 'unknown error');
        }
    }, 3000);
}

export async function forceFetchSubs(player: any, video: any) {
    await searchAndShowPicker(player, video, true);
}

function removePicker() { document.getElementById('subs-picker')?.remove(); }
function removeWhisperBtn() { document.getElementById('whisper-btn')?.remove(); }
