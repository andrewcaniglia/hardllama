import { useQuery } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  estimateAttachments,
  getChatContext,
  getInferenceCompute,
  getModelContextLength,
  getModelServer,
  getSettings,
} from "@/api";
import { estimateTokens } from "@/lib/tokens";

interface ContextCounterProps {
  chatId: string;
  model?: string;
  content: string;
  attachments: Array<{ id: string; filename: string; data?: Uint8Array }>;
}

// How full the context window can get before warning.
const NEAR_FULL = 0.8;

const format = (n: number) => n.toLocaleString("en-US");

export interface ContextCount {
  history: number;
  historyExact: boolean;
  prompt: number;
  attachments: number;
  attachmentCount: number;
  total: number;
  limit: number;
  limitExact: boolean;
  truncated: boolean;
  paired: boolean;
}

// ContextCounter shows how much of the model's context window the chat, the
// message being typed and its attachments will use, so it's clear when the
// context length needs raising. The chat so far is counted exactly by the
// engine after each reply; what hasn't been sent is estimated.
export function ContextCounter({
  chatId,
  model,
  content,
  attachments,
}: ContextCounterProps) {
  const navigate = useNavigate();
  const isNew = chatId === "new";
  const { data: chatContext } = useQuery({
    queryKey: ["chat", chatId, "context"],
    queryFn: () => getChatContext(chatId),
    enabled: !isNew,
  });
  const { data: settings } = useQuery({
    queryKey: ["settings"],
    queryFn: getSettings,
  });
  const { data: modelServer } = useQuery({
    queryKey: ["modelServer"],
    queryFn: getModelServer,
  });
  const { data: modelLength } = useQuery({
    queryKey: ["modelContextLength", model],
    queryFn: () => getModelContextLength(model!),
    enabled: !!model,
    staleTime: Infinity,
  });
  const { data: compute } = useQuery({
    queryKey: ["inferenceCompute"],
    queryFn: getInferenceCompute,
  });

  const newFiles = attachments.filter(
    (a): a is { id: string; filename: string; data: Uint8Array } => !!a.data,
  );
  const { data: fileEstimates } = useQuery({
    queryKey: ["attachmentEstimate", newFiles.map((a) => a.id)],
    queryFn: () => estimateAttachments(newFiles),
    enabled: newFiles.length > 0,
    staleTime: Infinity,
  });

  const count = countContext({
    isNew,
    model,
    content,
    usage: chatContext?.usage ?? null,
    estimate: chatContext?.estimate ?? 0,
    configured: settings?.settings.ContextLength ?? 0,
    defaultLength: compute?.defaultContextLength ?? 0,
    attachments:
      newFiles.length > 0
        ? (fileEstimates ?? []).reduce((sum, a) => sum + a.tokens, 0)
        : 0,
    attachmentCount: newFiles.length,
    paired: !!modelServer?.address,
    modelMax: modelLength?.max ?? 0,
  });

  if (isNew && !content.trim() && newFiles.length === 0) {
    return null;
  }

  const over = count.limit > 0 && count.total > count.limit;
  const near = count.limit > 0 && count.total >= count.limit * NEAR_FULL;
  const color = over
    ? "text-red-600 dark:text-red-400"
    : near
      ? "text-amber-600 dark:text-amber-400"
      : "text-neutral-500 dark:text-neutral-400";
  const approx = (exact: boolean) => (exact ? "" : "≈");
  const totalExact =
    count.historyExact && count.prompt === 0 && count.attachments === 0;

  const parts = [
    `History ${approx(count.historyExact)}${format(count.history)}`,
    `Prompt ≈${format(count.prompt)}`,
  ];
  if (count.attachmentCount > 0) {
    parts.push(
      `Attachments ≈${format(count.attachments)} (${count.attachmentCount} ${count.attachmentCount === 1 ? "file" : "files"})`,
    );
  }
  const limitText =
    count.limit > 0
      ? ` of ${approx(count.limitExact)}${format(count.limit)}`
      : "";

  let warning = "";
  if (over) {
    warning =
      "Too long for the context length. The model won't see the oldest messages.";
  } else if (count.truncated) {
    warning =
      "The model couldn't see the start of this chat in its last reply.";
  }

  return (
    <div
      aria-label="Context usage"
      className={`mt-1 px-3 text-xs tabular-nums ${color}`}
    >
      <span>
        {parts.join(" · ")} · {approx(totalExact)}
        {format(count.total)}
        {limitText}
      </span>
      {warning && (
        <span role="status">
          {" "}
          {warning}{" "}
          <button
            type="button"
            className="underline cursor-pointer"
            onClick={() => navigate({ to: "/settings" })}
          >
            Raise it in Settings
          </button>
        </span>
      )}
    </div>
  );
}

interface CountInput {
  isNew: boolean;
  model?: string;
  content: string;
  usage: {
    model: string;
    prompt: number;
    reply: number;
    limit: number;
    configured: number;
    truncated: boolean;
  } | null;
  estimate: number;
  configured: number;
  defaultLength: number;
  attachments: number;
  attachmentCount: number;
  // Models run on a paired computer.
  paired?: boolean;
  // The most context the model supports, 0 if unknown.
  modelMax?: number;
}

// countContext works out the counts shown. The engine's count is only used
// if it was made with the model selected now, and its limit only if the
// context length setting hasn't changed since. Otherwise the limit is the
// setting, capped at what the model supports.
export function countContext(input: CountInput): ContextCount {
  const { usage } = input;
  const sameModel = !input.isNew && !!usage && usage.model === input.model;
  const sameSetting = sameModel && usage!.configured === input.configured;

  const history = input.isNew
    ? 0
    : sameModel
      ? usage!.prompt + usage!.reply
      : input.estimate;
  const paired = !!input.paired;
  // What the engine will be asked for. This computer's default comes from
  // its own memory, so it says nothing about a paired computer's. A set
  // Context length is sent with each request, so it applies there too.
  const asked = paired
    ? input.configured
    : input.configured || input.defaultLength;
  const modelMax = input.modelMax ?? 0;
  let limit = asked;
  let limitExact = false;
  if (sameSetting && usage!.limit > 0) {
    // What the engine reported for the last reply.
    limit = usage!.limit;
    limitExact = true;
  } else if (asked > 0 && modelMax > 0) {
    // The engine caps the setting at the most the model supports.
    limit = Math.min(asked, modelMax);
    limitExact = true;
  }
  const prompt = estimateTokens(input.content);

  return {
    history,
    historyExact: input.isNew || sameModel,
    prompt,
    attachments: input.attachments,
    attachmentCount: input.attachmentCount,
    total: history + prompt + input.attachments,
    limit,
    limitExact,
    // Raising the context length may have fixed it.
    truncated: sameSetting && usage!.truncated,
    paired,
  };
}
