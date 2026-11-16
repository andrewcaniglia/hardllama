import { act, create } from "react-test-renderer";
import { describe, expect, it, vi } from "vitest";
import Thinking from "./Thinking";

vi.mock("./StreamingMarkdownContent", () => ({
  default: ({ content }: { content: string }) => <span>{content}</span>,
}));

vi.stubGlobal(
  "ResizeObserver",
  class {
    observe() {}
    disconnect() {}
  },
);

function render(props: Parameters<typeof Thinking>[0]) {
  let renderer: ReturnType<typeof create>;
  act(() => {
    renderer = create(<Thinking {...props} />);
  });
  return renderer!;
}

const label = (r: ReturnType<typeof create>) =>
  r.root.findByType("h3").children.join("");
const collapsedHeight = (r: ReturnType<typeof create>) =>
  r.root.findAll((n) => n.props.style?.maxHeight !== undefined)[0].props.style
    .maxHeight;

describe("Thinking", () => {
  const start = new Date(Date.now() - 5000);

  it("shows the end of the thinking while the reply streams", () => {
    const r = render({ thinking: "hmm", startTime: start, streaming: true });
    expect(label(r)).toBe("Thinking...");
    expect(collapsedHeight(r)).toBe("12rem");
  });

  it("collapses a finished reply with no recorded end time", () => {
    // Such as a reply that finished while HardLlama was locked.
    const r = render({ thinking: "hmm", startTime: start, streaming: false });
    expect(label(r)).toBe("Thought");
    expect(collapsedHeight(r)).toBe("0px");
  });

  it("shows how long it thought", () => {
    const r = render({
      thinking: "hmm",
      startTime: start,
      endTime: new Date(start.getTime() + 4000),
      streaming: false,
    });
    expect(label(r)).toBe("Thought for 4.0 seconds");
    expect(collapsedHeight(r)).toBe("0px");
  });
});
