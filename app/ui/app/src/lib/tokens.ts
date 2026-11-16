// Token estimates for the context counter. The model engine only counts
// exactly after a reply, so text that hasn't been sent is estimated. These
// match the estimates the app makes for attachments.

// What the engine counts each image as when deciding what fits.
export const IMAGE_TOKENS = 768;

// estimateTokens guesses how many tokens text is: about 4 characters each
// for English, and leaning high with one each for other characters, which
// often take more.
export function estimateTokens(text: string): number {
  let ascii = 0;
  let other = 0;
  for (const ch of text) {
    if (ch.charCodeAt(0) < 0x80) {
      ascii++;
    } else {
      other++;
    }
  }
  return Math.ceil(ascii / 4) + other;
}
