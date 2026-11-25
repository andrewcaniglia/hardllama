import { expect, it } from "vitest";
import { indexFolder, searchFolder, snapshotFolder, restoreFolder } from "./folderSearch";

it("retrieves matching nested text and excludes hidden files and unrelated passages", async () => {
  function file(path: string, text: string) {
    const file = new File([text], path.split("/").pop()!);
    Object.defineProperty(file, "webkitRelativePath", { value: path });
    return file;
  }
  const index = await indexFolder(
    [
      file("docs/guide/setup.md", "The deployment region is Iceland."),
      file("docs/notes.txt", "Coffee and cinnamon."),
      file("docs/.private/key.txt", "Deployment secret"),
    ],
    new AbortController().signal,
  );
  expect(index.files).toBe(2);
  const context = searchFolder(index, "deployment");
  expect(context).toContain("guide/setup.md");
  expect(context).toContain("Iceland");
  const restored = restoreFolder(JSON.parse(JSON.stringify(snapshotFolder(index))));
  expect(searchFolder(restored, "deployment")).toBe(context);
  expect(context).not.toMatch(/Coffee|secret/);
  expect(searchFolder(index, "bicycle")).toContain("no passages");
});
