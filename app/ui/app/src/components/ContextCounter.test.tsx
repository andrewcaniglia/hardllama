import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ContextCounter, countContext } from "./ContextCounter";

const mocks = vi.hoisted(() => ({
  getChatContext: vi.fn(),
  getSettings: vi.fn(),
  getInferenceCompute: vi.fn(),
  getModelServer: vi.fn(),
  getModelContextLength: vi.fn(),
  estimateAttachments: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@/api", () => mocks);
vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => mocks.navigate,
}));

const usage = {
  model: "llama",
  prompt: 2600,
  reply: 240,
  limit: 4096,
  configured: 0,
  messages: 4,
  truncated: false,
};

const base = {
  isNew: false,
  model: "llama",
  content: "",
  usage,
  estimate: 2000,
  configured: 0,
  defaultLength: 4096,
  attachments: 0,
  attachmentCount: 0,
};

describe("countContext", () => {
  it("uses the engine's count for the chat so far", () => {
    const c = countContext({ ...base, content: "a".repeat(400) });
    expect(c).toMatchObject({
      history: 2840,
      historyExact: true,
      prompt: 100,
      total: 2940,
      limit: 4096,
      limitExact: true,
    });
  });

  it("estimates the chat if it was counted with another model", () => {
    const c = countContext({ ...base, model: "qwen" });
    expect(c).toMatchObject({
      history: 2000,
      historyExact: false,
      limit: 4096,
      limitExact: false,
    });
  });

  it("uses the new setting once the context length changes", () => {
    const c = countContext({
      ...base,
      usage: { ...usage, truncated: true },
      configured: 16384,
    });
    expect(c).toMatchObject({
      limit: 16384,
      limitExact: false,
      truncated: false,
    });
  });

  it("knows the limit before any reply from the setting and the model", () => {
    const fresh = { ...base, usage: null, configured: 32768 };
    expect(countContext({ ...fresh, modelMax: 131072 })).toMatchObject({
      limit: 32768,
      limitExact: true,
    });
    // The engine caps the setting at what the model supports.
    expect(countContext({ ...fresh, modelMax: 8192 })).toMatchObject({
      limit: 8192,
      limitExact: true,
    });
    // Without the model's maximum, the setting may be capped.
    expect(countContext({ ...fresh, modelMax: 0 })).toMatchObject({
      limit: 32768,
      limitExact: false,
    });
    // Unset uses the engine's default.
    expect(
      countContext({ ...fresh, configured: 0, modelMax: 131072 }),
    ).toMatchObject({ limit: 4096, limitExact: true });
  });

  it("uses this computer's Context length for a paired computer", () => {
    // Before a reply from the other computer, the setting is what will be
    // sent with the request.
    expect(
      countContext({ ...base, usage: null, configured: 32768, paired: true }),
    ).toMatchObject({ limit: 32768, limitExact: false });
    // This computer's default says nothing about the other computer's.
    expect(
      countContext({ ...base, usage: null, configured: 0, paired: true }),
    ).toMatchObject({ limit: 0 });
  });

  it("counts nothing for a new chat but what's typed", () => {
    const c = countContext({
      ...base,
      isNew: true,
      usage: null,
      content: "abcd",
      attachments: 768,
      attachmentCount: 1,
    });
    expect(c).toMatchObject({ history: 0, prompt: 1, total: 769 });
  });
});

async function render(props: Parameters<typeof ContextCounter>[0]) {
  const queryClient = new QueryClient();
  let renderer: ReturnType<typeof create>;
  await act(async () => {
    renderer = create(
      <QueryClientProvider client={queryClient}>
        <ContextCounter {...props} />
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  return renderer!;
}

const text = (r: ReturnType<typeof create>) =>
  JSON.stringify(r.toJSON()).replace(/","/g, "");

describe("ContextCounter", () => {
  beforeEach(() => {
    for (const mock of Object.values(mocks)) mock.mockReset();
    mocks.getSettings.mockResolvedValue({ settings: { ContextLength: 0 } });
    mocks.getInferenceCompute.mockResolvedValue({ defaultContextLength: 4096 });
    mocks.getChatContext.mockResolvedValue({ usage, estimate: 2000 });
    mocks.getModelServer.mockResolvedValue({ address: "" });
    mocks.getModelContextLength.mockResolvedValue({ max: 0 });
  });

  it("shows history, prompt and attachments against the limit", async () => {
    mocks.estimateAttachments.mockResolvedValue([
      { filename: "a.txt", tokens: 300, image: false },
      { filename: "b.png", tokens: 768, image: true },
    ]);
    const r = await render({
      chatId: "c1",
      model: "llama",
      content: "a".repeat(40),
      attachments: [
        { id: "1", filename: "a.txt", data: new Uint8Array([1]) },
        { id: "2", filename: "b.png", data: new Uint8Array([2]) },
      ],
    });
    const t = text(r);
    expect(t).toContain("History 2,840");
    expect(t).toContain("Prompt ≈10");
    expect(t).toContain("Attachments ≈1,068 (2 files)");
    expect(t).toContain("≈3,918 of 4,096");
    expect(t).toContain("text-amber");
  });

  it("warns when it won't fit and links to Settings", async () => {
    const r = await render({
      chatId: "c1",
      model: "llama",
      content: "a".repeat(8000),
      attachments: [],
    });
    expect(text(r)).toContain("The model won't see the oldest messages.");
    expect(text(r)).toContain("text-red");

    await act(async () => {
      r.root.findByType("button").props.onClick();
    });
    expect(mocks.navigate).toHaveBeenCalledWith({ to: "/settings" });
  });

  it("says when the model already couldn't see the whole chat", async () => {
    mocks.getChatContext.mockResolvedValue({
      usage: { ...usage, truncated: true },
      estimate: 2000,
    });
    const r = await render({
      chatId: "c1",
      model: "llama",
      content: "",
      attachments: [],
    });
    expect(text(r)).toContain("couldn't see the start of this chat");
  });

  it("stays hidden for an empty new chat", async () => {
    const r = await render({
      chatId: "new",
      model: "llama",
      content: "",
      attachments: [],
    });
    expect(r.toJSON()).toBeNull();
    expect(mocks.getChatContext).not.toHaveBeenCalled();
  });
});
