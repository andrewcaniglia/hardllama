import type { Chat } from "@/gotypes";
import type { Content, TDocumentDefinitions } from "pdfmake/interfaces";

export type ExportFormat = "pdf" | "txt" | "md";

export function transcript(chat: Chat, reasoning: boolean) {
  const turns = [];
  for (const message of chat.messages ?? []) {
    const parts = [];
    if (reasoning && message.thinking) {
      parts.push({ label: "Reasoning", text: message.thinking });
    }
    if (message.content) {
      parts.push({
        label: parts.length ? "Response" : "",
        text: message.content,
      });
    }
    if (message.attachments?.length) {
      parts.push({
        label: "Attachments",
        text: message.attachments.map((file) => file.filename).join("\n"),
      });
    }
    if (parts.length) {
      const role = message.role;
      turns.push({ role: role.charAt(0).toUpperCase() + role.slice(1), parts });
    }
  }
  return { title: chat.title || "Chat transcript", turns };
}

export function exportText(chat: Chat, reasoning: boolean, markdown: boolean) {
  const { title, turns } = transcript(chat, reasoning);
  const blocks = [markdown ? "# " + title : title];
  for (const { role, parts } of turns) {
    blocks.push(markdown ? "## " + role : role);
    for (const { label, text } of parts) {
      if (label) blocks.push(markdown ? "### " + label : label);
      blocks.push(text);
    }
  }
  return blocks.join("\n\n") + "\n";
}

export async function exportPDF(
  chat: Chat,
  reasoning: boolean,
): Promise<string> {
  const [{ default: pdfMake }, { default: fonts }] = await Promise.all([
    import("pdfmake/build/pdfmake"),
    import("pdfmake/build/vfs_fonts"),
  ]);
  const { title, turns } = transcript(chat, reasoning);
  const content: Content[] = [{ text: title, style: "title" }];
  for (const { role, parts } of turns) {
    content.push({ text: role, style: "speaker" });
    for (const { label, text } of parts) {
      if (label)
        content.push({ text: label, bold: true, margin: [0, 6, 0, 3] });
      content.push({ text, preserveLeadingSpaces: true });
    }
  }
  const doc: TDocumentDefinitions = {
    info: { title },
    content,
    pageMargins: [48, 44, 48, 48],
    defaultStyle: { fontSize: 10.5, lineHeight: 1.25, color: "#20252b" },
    styles: {
      title: { fontSize: 21, bold: true, margin: [0, 0, 0, 8] },
      speaker: {
        fontSize: 13,
        bold: true,
        color: "#28546b",
        margin: [0, 14, 0, 4],
      },
    },
    pageBreakBefore: (node, following) =>
      node.style === "speaker" && following.length === 0,
    footer: (page, pages) => ({
      text: page + " / " + pages,
      alignment: "right",
      fontSize: 9,
      margin: [0, 14, 48, 0],
    }),
  };
  return new Promise((resolve) =>
    pdfMake.createPdf(doc, undefined, undefined, fonts).getBase64(resolve),
  );
}
