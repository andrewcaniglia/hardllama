import { describe, expect, it } from "vitest";
import { Chat } from "@/gotypes";
import { exportText, transcript } from "./chatExport";

const chat = new Chat({
  title: "Notes",
  messages: [
    {
      role: "user",
      content: "Read this",
      attachments: [{ filename: "notes.md", data: "RAW_ATTACHMENT" }],
    },
    { role: "assistant", thinking: "PRIVATE_TRACE", content: "An **answer**." },
    { role: "assistant", thinking: "REASONING_ONLY" },
  ],
});

describe("chat exports", () => {
  it.each([true, false])(
    "applies the reasoning toggle to text and Markdown (markdown=%s)",
    (markdown) => {
      const without = exportText(chat, false, markdown);
      expect(without).toContain("notes.md");
      expect(without).toContain("An **answer**.");
      expect(without).not.toMatch(
        /PRIVATE_TRACE|REASONING_ONLY|RAW_ATTACHMENT/,
      );
      expect(without.startsWith("# Notes")).toBe(markdown);
      expect(exportText(chat, true, markdown)).toContain("PRIVATE_TRACE");
      expect(exportText(chat, true, markdown)).toContain("REASONING_ONLY");
    },
  );

  it("omits an empty reasoning-only turn when reasoning is off", () => {
    expect(transcript(chat, false).turns).toHaveLength(2);
  });
});
