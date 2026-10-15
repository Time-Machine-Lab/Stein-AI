/**
 * JSON that a language model wrote by hand.
 *
 * Models asked for compact single-line JSON still emit literal line breaks
 * inside string values — Markdown instructions are the usual offender — and
 * `JSON.parse` rejects a raw control character inside a string. Every chat
 * reply protocol that carries structured data (the agent builder's
 * `<agent_draft>`, the graph builder's `<graph_draft>`) recovers the same way,
 * so the repair lives here instead of in each protocol.
 *
 * Only control characters *inside strings* are escaped. Object structure, the
 * quoting around every key, and all other syntax still have to pass
 * `JSON.parse`: a block that is genuinely malformed stays malformed rather than
 * being guessed at, because a half-parsed draft is worse than no draft.
 */
export function escapeJsonStringControlCharacters(value: string): string {
  let result = "";
  let inString = false;
  let escaped = false;

  for (const character of value) {
    if (!inString) {
      result += character;
      if (character === '"') inString = true;
      continue;
    }
    if (escaped) {
      result += character;
      escaped = false;
      continue;
    }
    if (character === "\\") {
      result += character;
      escaped = true;
      continue;
    }
    if (character === '"') {
      result += character;
      inString = false;
      continue;
    }
    if (character === "\n") {
      result += "\\n";
    } else if (character === "\r") {
      result += "\\r";
    } else if (character === "\t") {
      result += "\\t";
    } else {
      result += character;
    }
  }

  return result;
}
