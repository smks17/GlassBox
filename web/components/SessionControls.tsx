"use client";

import { useState } from "react";
import { snapshot } from "@/lib/wasm-bridge";
import { saveSession } from "@/lib/backend";
import { version } from "../package.json";
import styles from "./SessionControls.module.css";

export default function SessionControls() {
  const [link, setLink] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  async function handleSave() {
    if (!window.confirm("Anyone with the link can open this session. Files and environment variables may still contain secrets. Share this session?")) return;
    setError(null);
    try {
      const bytes = snapshot();
      if (!bytes) throw new Error("nothing to save");
      const id = await saveSession(bytes);
      const url = new URL(window.location.href);
      url.searchParams.set("session", id);
      setLink(url.toString());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  return (
    <div className={styles.controls}>
      <span>GlassBox v{version}</span>
      <button className={styles.button} onClick={handleSave}>
        save session
      </button>
      {link && (
        <a className={styles.link} href={link}>
          {link}
        </a>
      )}
      {error && <span className={styles.error}>{error}</span>}
    </div>
  );
}
