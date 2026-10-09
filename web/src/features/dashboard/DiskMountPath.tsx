import { useRef, useState } from "preact/hooks";

export function DiskMountPath({
  path,
  index,
}: {
  path: string;
  index: number;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [result, setResult] = useState<"" | "copied" | "selected">("");
  async function copy() {
    try {
      if (!navigator.clipboard) throw new Error("Clipboard unavailable");
      await navigator.clipboard.writeText(path);
      setResult("copied");
    } catch {
      input.current?.focus();
      input.current?.select();
      setResult("selected");
    }
  }
  return (
    <div class="disk-mount-path">
      <label>
        <span>Location {index}</span>
        <input
          ref={input}
          aria-label={"Mount path " + index}
          value={path}
          readOnly
          spellcheck={false}
          onFocus={(event) => event.currentTarget.select()}
        />
      </label>
      <button
        type="button"
        aria-label={"Copy mount path " + index}
        onClick={copy}
      >
        {result === "copied" ? "Copied" : "Copy"}
      </button>
      {result && (
        <span class="disk-copy-result" role="status">
          {result === "copied"
            ? "Path copied."
            : "Path selected. Use your device’s copy command."}
        </span>
      )}
    </div>
  );
}
