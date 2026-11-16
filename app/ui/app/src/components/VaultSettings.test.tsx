import { act, create } from "react-test-renderer";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { VaultSettings } from "./VaultSettings";

const mocks = vi.hoisted(() => ({
  changeVaultPassword: vi.fn(),
  lockVault: vi.fn(),
}));

vi.mock("@/api", () => ({
  changeVaultPassword: mocks.changeVaultPassword,
  lockVault: mocks.lockVault,
}));

const NEW_LABEL = "New password (at least 12 characters)";

function render(queryClient: QueryClient) {
  let renderer: ReturnType<typeof create>;
  act(() => {
    renderer = create(
      <QueryClientProvider client={queryClient}>
        <VaultSettings />
      </QueryClientProvider>,
    );
  });
  return renderer!;
}

function buttons(renderer: ReturnType<typeof create>, text: string) {
  return renderer.root.findAll(
    (node) =>
      typeof node.type !== "string" &&
      node.props.onClick !== undefined &&
      node.props.children === text,
  );
}

async function change(
  renderer: ReturnType<typeof create>,
  current: string,
  password: string,
  confirmation: string,
) {
  act(() => {
    buttons(renderer, "Change password")[0].props.onClick();
  });
  const type = (label: string, value: string) =>
    act(() => {
      renderer.root
        .findByProps({ "aria-label": label, type: "password" })
        .props.onChange({ target: { value } });
    });
  type("Current password", current);
  type(NEW_LABEL, password);
  type("Confirm new password", confirmation);
  await act(async () => {
    await renderer.root
      .findByType("form")
      .props.onSubmit({ preventDefault: () => {} });
  });
}

const alert = (renderer: ReturnType<typeof create>) =>
  renderer.root.findByProps({ role: "alert" }).children;

describe("VaultSettings", () => {
  beforeEach(() => {
    mocks.changeVaultPassword.mockReset();
    mocks.lockVault.mockReset();
  });

  it("hides the form until asked", () => {
    const renderer = render(new QueryClient());
    expect(renderer.root.findAllByType("form")).toHaveLength(0);
  });

  it("refuses short new passwords", async () => {
    const renderer = render(new QueryClient());
    await change(renderer, "old password!", "11-char-pwd", "11-char-pwd");

    expect(mocks.changeVaultPassword).not.toHaveBeenCalled();
    expect(alert(renderer)).toEqual([
      "Password must be at least 12 characters",
    ]);
  });

  it("refuses new passwords that don't match", async () => {
    const renderer = render(new QueryClient());
    await change(renderer, "old password!", "correct horse", "correct hose");

    expect(mocks.changeVaultPassword).not.toHaveBeenCalled();
    expect(alert(renderer)).toEqual(["New passwords don't match"]);
  });

  it("shows server errors and keeps the form open", async () => {
    mocks.changeVaultPassword.mockRejectedValue(
      new Error("incorrect password"),
    );
    const renderer = render(new QueryClient());
    await change(renderer, "wrong!", "correct horse", "correct horse");

    expect(alert(renderer)).toEqual(["incorrect password"]);
    expect(renderer.root.findAllByType("form")).toHaveLength(1);
  });

  it("changes the password and clears the form", async () => {
    const status = { enabled: true, locked: false };
    mocks.changeVaultPassword.mockResolvedValue(status);
    const queryClient = new QueryClient();
    const renderer = render(queryClient);
    await change(renderer, "old password!", "correct horse", "correct horse");

    expect(mocks.changeVaultPassword).toHaveBeenCalledWith(
      "old password!",
      "correct horse",
    );
    expect(queryClient.getQueryData(["vault"])).toEqual(status);
    expect(renderer.root.findAllByType("form")).toHaveLength(0);
    expect(renderer.root.findByProps({ role: "status" }).children).toEqual([
      "Password changed",
    ]);

    // Reopening the form doesn't bring back what was typed.
    act(() => {
      buttons(renderer, "Change password")[0].props.onClick();
    });
    for (const label of [
      "Current password",
      NEW_LABEL,
      "Confirm new password",
    ]) {
      expect(
        renderer.root.findByProps({ "aria-label": label, type: "password" })
          .props.value,
      ).toBe("");
    }
  });
});
