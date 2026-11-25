import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { LockDelaySettings } from "./LockDelaySettings";
import { Slider } from "@/components/ui/slider";

const mocks = vi.hoisted(() => ({
  getLockDelay: vi.fn(),
  setLockDelay: vi.fn(),
}));

vi.mock("@/api", () => mocks);

async function render() {
  const queryClient = new QueryClient();
  let renderer: ReturnType<typeof create>;
  await act(async () => {
    renderer = create(
      <QueryClientProvider client={queryClient}>
        <LockDelaySettings />
      </QueryClientProvider>,
    );
  });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
  return renderer!;
}

const slider = (renderer: ReturnType<typeof create>) =>
  renderer.root.findByType(Slider);

describe("LockDelaySettings", () => {
  beforeEach(() => {
    mocks.getLockDelay.mockReset();
    mocks.setLockDelay.mockReset();
  });

  it("offers instantly to 15 minutes and shows the saved choice", async () => {
    mocks.getLockDelay.mockResolvedValue({ seconds: 30 });
    const renderer = await render();
    expect(
      slider(renderer).props.options.map((o: { value: number }) => o.value),
    ).toEqual([0, 5, 15, 30, 60, 300, 600, 900]);
    expect(slider(renderer).props.value).toBe(30);
  });

  it.each([300, 600, 900])("saves a delay of %i seconds", async (seconds) => {
    mocks.getLockDelay.mockResolvedValue({ seconds: 5 });
    mocks.setLockDelay.mockResolvedValue({ seconds });
    const renderer = await render();

    await act(async () => {
      await slider(renderer).props.onChange(seconds);
    });

    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(mocks.setLockDelay).toHaveBeenCalledWith(seconds);
    expect(slider(renderer).props.value).toBe(seconds);
  });

  it("shows why a change failed and keeps the saved choice", async () => {
    mocks.getLockDelay.mockResolvedValue({ seconds: 5 });
    mocks.setLockDelay.mockRejectedValue(new Error("chats are locked"));
    const renderer = await render();

    await act(async () => {
      await slider(renderer).props.onChange(60);
    });
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });

    expect(JSON.stringify(renderer.toJSON())).toContain("chats are locked");
    expect(slider(renderer).props.value).toBe(5);
  });
});
