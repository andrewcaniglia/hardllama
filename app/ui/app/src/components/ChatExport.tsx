import { ChevronDownIcon } from "@heroicons/react/24/outline";
import { Popover, PopoverButton, PopoverPanel } from "@headlessui/react";
import { useEffect, useRef, useState } from "react";
import { uint8ArrayToBase64 } from "@/api";
import type { Chat } from "@/gotypes";
import { exportPDF, exportText, type ExportFormat } from "@/lib/chatExport";

const formats = [
  ["pdf", "PDF (.pdf)"],
  ["txt", "Plain text (.txt)"],
  ["md", "Markdown (.md)"],
] as const;

export function ChatExport({ chat }: { chat: Chat }) {
  const [reasoning, setReasoning] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  async function save(format: ExportFormat, close: () => void) {
    setError("");
    setBusy(true);
    try {
      if (!window.saveChatExport)
        throw new Error("Open HardLlama’s desktop app to save an export.");
      const data =
        format === "pdf"
          ? await exportPDF(chat, reasoning)
          : uint8ArrayToBase64(
              new TextEncoder().encode(
                exportText(chat, reasoning, format === "md"),
              ),
            );
      if (!mounted.current) return;
      if (await window.saveChatExport(format, data)) close();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Popover className="fixed right-4 top-2 z-30">
      {({ open, close }) => (
        <>
          <PopoverButton
            disabled={busy}
            onClick={() => {
              if (!open) {
                setReasoning(false);
                setError("");
              }
            }}
            className="flex h-9 items-center gap-1.5 rounded-full bg-white px-3 text-sm text-neutral-700 hover:bg-neutral-100 dark:bg-neutral-900 dark:text-neutral-200 dark:hover:bg-neutral-800"
          >
            {busy ? "Preparing…" : "Export chat"}
            <ChevronDownIcon aria-hidden="true" className="size-4" />
          </PopoverButton>
          <PopoverPanel
            focus
            role="dialog"
            aria-label="Export chat"
            className="absolute right-0 mt-1 w-60 rounded-xl border border-neutral-200 bg-white p-2 text-sm shadow-lg dark:border-neutral-700 dark:bg-neutral-800"
          >
            <label className="flex items-center gap-2 border-b border-neutral-200 px-2 py-2 dark:border-neutral-700">
              <input
                type="checkbox"
                checked={reasoning}
                disabled={busy}
                onChange={(e) => setReasoning(e.target.checked)}
              />
              Include reasoning traces
            </label>
            {formats.map(([value, label]) => (
              <button
                key={value}
                type="button"
                disabled={busy}
                className="block w-full rounded-md px-2 py-2 text-left hover:bg-neutral-100 focus-visible:bg-neutral-100 dark:hover:bg-neutral-700 dark:focus-visible:bg-neutral-700"
                onClick={() => void save(value, close)}
              >
                {label}
              </button>
            ))}
            <p className="px-2 py-1 text-xs text-neutral-500 dark:text-neutral-400">
              Exported files are readable outside the encrypted vault.
            </p>
            {error && (
              <p role="alert" className="px-2 py-1 text-red-600">
                {error}
              </p>
            )}
          </PopoverPanel>
        </>
      )}
    </Popover>
  );
}
