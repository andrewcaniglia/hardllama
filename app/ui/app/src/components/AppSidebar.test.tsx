import { act, create } from "react-test-renderer";
import { describe, expect, it, vi } from "vitest";
import { AppNavigation } from "./AppSidebar";

vi.mock("@/components/ui/link", () => ({
  Link: ({ to, children }: { to: string; children: React.ReactNode }) => (
    <a data-to={to}>{children}</a>
  ),
}));

describe("AppNavigation", () => {
  it("has no Apps page in HardLlama", () => {
    let renderer!: ReturnType<typeof create>;
    act(() => {
      renderer = create(<AppNavigation current="chat" />);
    });
    const links = renderer.root
      .findAllByType("a")
      .map((a) => a.props["data-to"]);
    expect(links).not.toContain("/connect");
    expect(links).toContain("/settings");
  });
});
