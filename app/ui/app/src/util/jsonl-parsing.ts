export async function* parseJsonlFromStream<T>(
  stream: ReadableStream<Uint8Array>,
  strict = false,
): AsyncGenerator<T, void, unknown> {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";

  try {
    while (true) {
      const { done, value } = await reader.read();

      if (done) {
        // Process any remaining data in buffer
        if (buffer.trim()) {
          try {
            yield JSON.parse(buffer.trim());
          } catch {
            if (strict) throw new Error("Invalid data in the chat response.");
            console.error("Invalid final JSON frame");
          }
        }
        break;
      }

      // Decode the chunk and add to buffer
      buffer += decoder.decode(value, { stream: true });

      // Process complete lines
      const lines = buffer.split("\n");
      buffer = lines.pop() || ""; // Keep incomplete line in buffer

      for (const line of lines) {
        const trimmed = line.trim();
        if (trimmed) {
          try {
            yield JSON.parse(trimmed);
          } catch {
            if (strict) throw new Error("Invalid data in the chat response.");
            console.error("Invalid JSON frame");
          }
        }
      }
    }
  } finally {
    reader.releaseLock();
  }
}

/**
 * Helper function to parse JSONL from a Response object
 */
export async function* parseJsonlFromResponse<T>(
  response: Response,
  strict = false,
): AsyncGenerator<T, void, unknown> {
  if (!response.body) {
    throw new Error("Response body is null");
  }
  yield* parseJsonlFromStream<T>(response.body, strict);
}
