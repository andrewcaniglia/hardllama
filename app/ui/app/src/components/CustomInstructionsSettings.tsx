import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ChatBubbleBottomCenterTextIcon } from "@heroicons/react/24/outline";
import { getCustomInstructions, setCustomInstructions } from "@/api";
import { Button } from "@/components/ui/button";
import { Description, Field, Label } from "@/components/ui/fieldset";

export const MAX_CUSTOM_INSTRUCTIONS = 20000;

// CustomInstructionsSettings edits instructions sent with every chat, after
// the model's own system prompt.
export function CustomInstructionsSettings() {
  const queryClient = useQueryClient();
  const { data } = useQuery({
    queryKey: ["customInstructions"],
    queryFn: getCustomInstructions,
  });
  const saved = data?.instructions ?? "";
  const [text, setText] = useState(saved);
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [isSaving, setIsSaving] = useState(false);

  // Show what's saved once it loads.
  useEffect(() => setText(saved), [saved]);

  const handleSave = async () => {
    setIsSaving(true);
    setError("");
    setMessage("");
    try {
      queryClient.setQueryData(
        ["customInstructions"],
        await setCustomInstructions(text),
      );
      setMessage("Saved. They apply to your next message.");
    } catch (err) {
      setError(
        err instanceof Error ? err.message : "Failed to save instructions",
      );
    } finally {
      setIsSaving(false);
    }
  };

  const tooLong = text.length > MAX_CUSTOM_INSTRUCTIONS;

  return (
    <Field>
      <div className="flex items-start space-x-3">
        <ChatBubbleBottomCenterTextIcon className="mt-1 h-5 w-5 flex-shrink-0 text-black dark:text-neutral-100" />
        <div className="w-full">
          <Label>Custom instructions</Label>
          <Description>
            Sent with every message in every chat, after the model's own
            instructions. Encrypted like your chats.
          </Description>
          <textarea
            aria-label="Custom instructions"
            value={text}
            disabled={!data}
            onChange={(e) => {
              setText(e.target.value);
              setMessage("");
            }}
            rows={5}
            placeholder="For example: Keep answers short. Time has passed since your training data ends, so if I mention something you haven't heard of, don't call it wrong."
            className="mt-2 block w-full resize-y rounded-lg border border-zinc-950/10 bg-transparent px-3 py-2 text-sm text-zinc-950 placeholder:text-zinc-500 focus:outline-none focus:ring-2 focus:ring-blue-500 dark:border-white/10 dark:text-white"
          />
          <div className="mt-2 flex items-center gap-3">
            <Button
              type="button"
              disabled={isSaving || !data || text === saved || tooLong}
              onClick={() => void handleSave()}
            >
              {isSaving ? "Saving..." : "Save"}
            </Button>
            {tooLong && (
              <span className="text-sm text-red-600 dark:text-red-400">
                {`At most ${MAX_CUSTOM_INSTRUCTIONS.toLocaleString("en-US")} characters`}
              </span>
            )}
            {message && (
              <span
                role="status"
                className="text-sm text-neutral-500 dark:text-neutral-400"
              >
                {message}
              </span>
            )}
          </div>
          {error && (
            <p
              role="alert"
              className="mt-1 text-sm text-red-600 dark:text-red-400"
            >
              {error}
            </p>
          )}
        </div>
      </div>
    </Field>
  );
}
