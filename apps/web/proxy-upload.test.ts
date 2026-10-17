import { createHash } from "node:crypto";
import type { IncomingMessage } from "node:http";
import { createRequire } from "node:module";
import { Readable } from "node:stream";
import { describe, expect, it, vi } from "vitest";
import { getCloneableBody } from "next/dist/server/body-streams";

vi.mock("fumadocs-mdx/next", () => ({
  createMDX: () => (config: unknown) => config,
}));

import nextConfig from "./next.config";

const require = createRequire(import.meta.url);
const bytes = require("next/dist/compiled/bytes") as {
  parse: (value: string | number) => number;
};

async function readBytes(stream: Readable) {
  const chunks: Buffer[] = [];
  for await (const chunk of stream) chunks.push(Buffer.from(chunk));
  return Buffer.concat(chunks);
}

const digest = (data: Buffer) => createHash("sha256").update(data).digest("hex");

describe("API upload body through Next.js proxy", () => {
  it.each([512, 16 * 1024 * 1024])(
    "preserves all %i bytes for both proxy inspection and upstream forwarding",
    async (size) => {
      const payload = Buffer.alloc(size, 0x41);
      Buffer.from("multipart upload tail must reach the Go backend").copy(
        payload,
        size - 64,
      );
      const chunks = [];
      for (let offset = 0; offset < size; offset += 512 * 1024) {
        chunks.push(payload.subarray(offset, offset + 512 * 1024));
      }
      const request = Object.assign(Readable.from(chunks), {
        url: "/api/upload-file",
      }) as IncomingMessage;
      const configuredLimit = nextConfig.experimental?.proxyClientMaxBodySize;
      const body = getCloneableBody(
        request,
        configuredLimit === undefined ? undefined : bytes.parse(configuredLimit),
      );

      const inspected = await readBytes(body.cloneBodyStream());
      await body.finalize();
      const forwarded = await readBytes(request);

      expect(inspected.length).toBe(payload.length);
      expect(digest(inspected)).toBe(digest(payload));
      expect(forwarded.length).toBe(payload.length);
      expect(digest(forwarded)).toBe(digest(payload));
    },
  );
});
