import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useReplyFinishing } from "./useReplyFinishing";

const mocks = vi.hoisted(() => ({
  getReplyFinishing: vi.fn(),
}));

vi.mock("@/api", () => mocks);

function Probe({ chatId }: { chatId: string }) {
  return <span>{useReplyFinishing(chatId) ? "finishing" : "idle"}</span>;
}

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

describe("useReplyFinishing", () => {
  beforeEach(() => mocks.getReplyFinishing.mockReset());

  it("refreshes the chat until the reply is done", async () => {
    mocks.getReplyFinishing.mockResolvedValue({ finishing: true });
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    let renderer: ReturnType<typeof create>;
    await act(async () => {
      renderer = create(
        <QueryClientProvider client={queryClient}>
          <Probe chatId="c1" />
        </QueryClientProvider>,
      );
    });
    await flush();
    expect(JSON.stringify(renderer!.toJSON())).toContain("finishing");
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: ["chat", "c1"],
      exact: true,
    });

    // It finishes: refreshed once more, then left alone.
    mocks.getReplyFinishing.mockResolvedValue({ finishing: false });
    invalidate.mockClear();
    await act(async () => {
      await queryClient.refetchQueries({
        queryKey: ["chat", "c1", "finishing"],
      });
    });
    await flush();
    expect(JSON.stringify(renderer!.toJSON())).toContain("idle");
    expect(invalidate).toHaveBeenCalledTimes(1);
  });

  it("does nothing for a chat with no reply finishing", async () => {
    mocks.getReplyFinishing.mockResolvedValue({ finishing: false });
    const queryClient = new QueryClient();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");
    await act(async () => {
      create(
        <QueryClientProvider client={queryClient}>
          <Probe chatId="c1" />
        </QueryClientProvider>,
      );
    });
    await flush();
    expect(invalidate).not.toHaveBeenCalled();
  });
});
