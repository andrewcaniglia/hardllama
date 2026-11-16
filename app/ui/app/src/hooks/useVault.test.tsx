import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useVaultStatus } from "./useVault";

const mocks = vi.hoisted(() => ({
  getVaultStatus: vi.fn(),
  reportActivity: vi.fn(),
}));

vi.mock("@/api", () => mocks);

async function flush() {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

function Probe() {
  const { data } = useVaultStatus();
  return <span>{data?.locked ? "locked" : "unlocked"}</span>;
}

async function render() {
  const queryClient = new QueryClient();
  queryClient.setQueryData(["chats"], ["secret chat"]);
  let renderer: ReturnType<typeof create>;
  await act(async () => {
    renderer = create(
      <QueryClientProvider client={queryClient}>
        <Probe />
      </QueryClientProvider>,
    );
  });
  await flush();
  return { renderer: renderer!, queryClient };
}

describe("useVaultStatus", () => {
  beforeEach(() => {
    mocks.getVaultStatus.mockReset();
    mocks.reportActivity.mockReset().mockResolvedValue(undefined);
    // Tests run without a browser.
    vi.stubGlobal("window", new EventTarget());
    return () => vi.unstubAllGlobals();
  });

  it("shows the lock screen as soon as the app locks chats", async () => {
    // The server's answer is held back, so only the app's call can lock.
    mocks.getVaultStatus.mockResolvedValueOnce({
      enabled: true,
      locked: false,
    });
    const { renderer, queryClient } = await render();
    expect(JSON.stringify(renderer.toJSON())).toContain("unlocked");
    mocks.getVaultStatus.mockReturnValue(new Promise(() => {}));

    await act(async () => {
      window.__hardllamaLocked?.();
    });

    await flush();
    expect(JSON.stringify(renderer.toJSON())).toContain('"locked"');
    expect(queryClient.getQueryData(["chats"])).toBeUndefined();
  });

  it("does nothing without a password", async () => {
    mocks.getVaultStatus.mockResolvedValue({ enabled: false, locked: false });
    const { renderer } = await render();

    await act(async () => {
      window.__hardllamaLocked?.();
    });
    await flush();

    expect(JSON.stringify(renderer.toJSON())).toContain("unlocked");
  });

  it("stops listening when unmounted", async () => {
    mocks.getVaultStatus.mockResolvedValue({ enabled: true, locked: false });
    const { renderer } = await render();
    act(() => renderer.unmount());
    expect(window.__hardllamaLocked).toBeUndefined();
  });

  it("tells the idle lock about typing, at most every 30 seconds", async () => {
    mocks.getVaultStatus.mockResolvedValue({ enabled: true, locked: false });
    await render();

    window.dispatchEvent(new Event("keydown"));
    window.dispatchEvent(new Event("keydown"));
    window.dispatchEvent(new Event("pointerdown"));
    expect(mocks.reportActivity).toHaveBeenCalledTimes(1);
  });

  it("doesn't report activity while locked", async () => {
    mocks.getVaultStatus.mockResolvedValue({ enabled: true, locked: true });
    await render();

    window.dispatchEvent(new Event("keydown"));
    expect(mocks.reportActivity).not.toHaveBeenCalled();
  });
});
