import { useEffect, useRef } from "react";
import { saveDraft, takeDraft } from "@/api";

interface DraftOptions {
  chatId: string;
  content: string;
  // Drafts of private chats and of edits to earlier messages aren't kept.
  skip: boolean;
  restore: (content: string) => void;
}

// useDraft keeps what the user is typing when the chat form goes away, such
// as when chats lock, and puts it back when the form returns to that chat.
// The app keeps drafts encrypted in memory, so they can be saved while
// locked but only read after unlocking.
export function useDraft({ chatId, content, skip, restore }: DraftOptions) {
  const latest = useRef({ content, skip, restore });
  latest.current = { content, skip, restore };

  useEffect(() => {
    let cancelled = false;
    takeDraft(chatId)
      .then(({ content }) => {
        if (!cancelled && content && !latest.current.skip) {
          latest.current.restore(content);
        }
      })
      .catch(() => {});

    return () => {
      cancelled = true;
      // Runs before the form clears itself for another chat, so this is
      // still what was typed in chatId.
      const { content, skip } = latest.current;
      if (!skip && content.trim()) {
        saveDraft(chatId, content).catch(() => {});
      }
    };
  }, [chatId]);
}
