import { PlayerState } from '../core/events.svelte';
import type { SubsInfo, LocalTrack } from './subs';
import type { AudioTrack } from './AudioPicker.svelte';

export class SubsManager {
  info = $state<SubsInfo | null>(null);
  open = $state(false);
  activeSub = $state<string | null>(null);
  onlineResults = $state<any[] | null>(null);
  searching = $state(false);

  toggle() { this.open = !this.open; }
  close() { this.open = false; }

  setInfo(info: SubsInfo | null) { this.info = info; }
  markActive(label: string | null) { this.activeSub = label; }
  setResults(r: any[] | null) { this.onlineResults = r; }
  setSearching(b: boolean) { this.searching = b; }
}

export class AudioManager {
  tracks = $state<AudioTrack[]>([]);
  active = $state(0);
  open = $state(false);

  toggle() { this.open = !this.open; }
  close() { this.open = false; }

  setTracks(tracks: AudioTrack[]) { this.tracks = tracks; }
  select(track: number) { this.active = track; }
}

export class PlayerView {
  state = new PlayerState();
  subs = new SubsManager();
  audio = new AudioManager();

  destroy() {
    this.state.destroy();
  }
}

export type { LocalTrack };
