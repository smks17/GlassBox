export interface ArchitectureSource {
  getHighEntropyValues(hints: string[]): Promise<{
    architecture?: string;
    bitness?: string;
    platform?: string;
  }>;
}

// Missing or withheld hints stay unknown rather than being guessed from a UA.
export async function detectHostMachine(
  source: ArchitectureSource | undefined,
  timeoutMs = 1000,
): Promise<string> {
  if (!source) return "unknown";
  let timeout: ReturnType<typeof setTimeout> | undefined;
  try {
    const values = await Promise.race([
      source.getHighEntropyValues(["architecture", "bitness"]),
      new Promise<null>((resolve) => {
        timeout = setTimeout(() => resolve(null), timeoutMs);
      }),
    ]);
    if (values?.architecture === "arm") {
      if (values.bitness === "64") {
        return values.platform === "Linux" || values.platform === "Android"
          ? "aarch64"
          : "arm64";
      }
      if (values.bitness === "32") return "arm";
    }
    if (values?.architecture === "x86") {
      if (values.bitness === "64") return "x86_64";
      if (values.bitness === "32") return "i686";
    }
    return "unknown";
  } catch {
    return "unknown";
  } finally {
    clearTimeout(timeout);
  }
}
