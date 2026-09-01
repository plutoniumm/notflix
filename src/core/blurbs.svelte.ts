import { GET } from "./api";

// Blurbs land whenever the background sweep gets to them, and most of the
// library will have none for a while — so this is a lazy singleton: one silent
// fetch per page load, shared by every card and the player. A miss is the
// normal state and must never reach the user as an error.
class Blurbs {
  map = $state<Record<string, string>>({});
  private pending: Promise<void> | null = null;

  load(): Promise<void> {
    this.pending ??= GET("/api/blurbs", { silent: true, tag: "blurbs" }).then((res: any) => {
      if (res && typeof res === "object") this.map = res;
    });

    return this.pending;
  }

  get(key: string): string {
    return this.map[key] ?? "";
  }
}

export const blurbs = new Blurbs();
