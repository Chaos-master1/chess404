// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { buildUpstreamHeaders, proxyInternalServiceStream } from "./internal-service";

const streamConfig = {
  fallbackUrl: "http://platform-service.railway.internal:8080",
  envName: "PLATFORM_SERVICE_INTERNAL_URL",
  serviceName: "platform-service",
} as const;

afterEach(() => {
  // Node's global fetch has no default mock; every stream test installs its own.
  vi.unstubAllGlobals();
});

function makeRequest(headers: Record<string, string>, url = "https://web-production-9a697.up.railway.app/api/gateway/bootstrap"): Request {
  return new Request(url, { method: "POST", headers });
}

describe("buildUpstreamHeaders", () => {
  it("injects X-Forwarded-Proto and X-Forwarded-Host from the request URL when not present", () => {
    const req = makeRequest({ host: "web-production-9a697.up.railway.app" });
    const out = buildUpstreamHeaders(req);
    expect(out.get("x-forwarded-proto")).toBe("https");
    expect(out.get("x-forwarded-host")).toBe("web-production-9a697.up.railway.app");
  });

  it("sets Origin from the public URL when the browser did not send one", () => {
    const req = makeRequest({ host: "web-production-9a697.up.railway.app" });
    const out = buildUpstreamHeaders(req);
    expect(out.get("origin")).toBe("https://web-production-9a697.up.railway.app");
  });

  it("preserves the browser's Origin if it was set (cross-origin POST)", () => {
    const req = makeRequest({
      host: "web-production-9a697.up.railway.app",
      origin: "https://other.example.com",
    });
    const out = buildUpstreamHeaders(req);
    expect(out.get("origin")).toBe("https://other.example.com");
  });

  it("preserves existing X-Forwarded-Proto/Host from upstream proxy chain", () => {
    const req = makeRequest({
      host: "gateway.railway.internal",
      "x-forwarded-proto": "https",
      "x-forwarded-host": "web-production-9a697.up.railway.app",
    });
    const out = buildUpstreamHeaders(req);
    expect(out.get("x-forwarded-proto")).toBe("https");
    expect(out.get("x-forwarded-host")).toBe("web-production-9a697.up.railway.app");
    expect(out.get("origin")).toBe("https://web-production-9a697.up.railway.app");
  });

  it("strips hop-by-hop and oversized headers (host, connection, content-length)", () => {
    const req = makeRequest({
      host: "web-production-9a697.up.railway.app",
      connection: "close",
      "content-length": "1234",
      "x-custom": "keep-me",
    });
    const out = buildUpstreamHeaders(req);
    expect(out.get("host")).toBeNull();
    expect(out.get("connection")).toBeNull();
    expect(out.get("content-length")).toBeNull();
    expect(out.get("x-custom")).toBe("keep-me");
  });

  it("uses http when the request URL is http", () => {
    const req = makeRequest({ host: "localhost:3000" }, "http://localhost:3000/api/gateway/bootstrap");
    const out = buildUpstreamHeaders(req);
    expect(out.get("x-forwarded-proto")).toBe("http");
    expect(out.get("origin")).toBe("http://localhost:3000");
  });

  it("adds the configured internal service token for backend hops", () => {
    const previous = process.env.GATEWAY_INTERNAL_SERVICE_TOKEN;
    process.env.GATEWAY_INTERNAL_SERVICE_TOKEN = "gateway-test-token";
    try {
      const req = makeRequest({ host: "web-production-9a697.up.railway.app" });
      const out = buildUpstreamHeaders(req);
      expect(out.get("x-chess404-service-token")).toBe("gateway-test-token");
    } finally {
      if (previous === undefined) {
        delete process.env.GATEWAY_INTERNAL_SERVICE_TOKEN;
      } else {
        process.env.GATEWAY_INTERNAL_SERVICE_TOKEN = previous;
      }
    }
  });
});

describe("proxyInternalServiceStream", () => {
  function makeStreamRequest(): Request {
    return new Request("https://web.test/api/platform/inbox/stream", {
      method: "POST",
      body: JSON.stringify({}),
      headers: { "content-type": "application/json" },
    });
  }

  function stubFetchWithSlowBody(chunks: string[]): void {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        const encoder = new TextEncoder();
        const body = new ReadableStream<Uint8Array>({
          start(controller) {
            let i = 0;
            const push = () => {
              if (i < chunks.length) {
                controller.enqueue(encoder.encode(chunks[i++]));
                setTimeout(push, 50);
              } else {
                controller.close();
              }
            };
            push();
          },
        });
        return new Response(body, {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        });
      }),
    );
  }

  it("keeps streaming the body after the establishment budget elapses (long-poll survival)", async () => {
    stubFetchWithSlowBody(["data: a\n\n", "data: b\n\n", "data: c\n\n"]);
    const response = await proxyInternalServiceStream(makeStreamRequest(), "/inbox/stream", streamConfig, 30);
    expect(response.status).toBe(200);
    const reader = response.body!.getReader();
    const decoder = new TextDecoder();
    let text = "";
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      text += decoder.decode(value, { stream: true });
    }
    expect(text).toContain("data: a");
    expect(text).toContain("data: c");
  }, 10_000);

  it("aborts upstream when headers never arrive within the establishment budget", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(
        (_url: string, init?: RequestInit) =>
          new Promise<Response>((_resolve, reject) => {
            init?.signal?.addEventListener("abort", () =>
              reject(new DOMException("The operation was aborted.", "AbortError")),
            );
          }),
      ),
    );
    const response = await proxyInternalServiceStream(makeStreamRequest(), "/inbox/stream", streamConfig, 30);
    expect(response.status).toBe(502);
    const payload = (await response.json()) as { error?: string };
    expect(payload.error).toContain("unreachable");
  }, 10_000);

  it("propagates the client method to the upstream request", async () => {
    const fetchMock = vi.fn(async (_url: string, init?: RequestInit) => new Response("ok", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await proxyInternalServiceStream(makeStreamRequest(), "/inbox/stream", streamConfig, 5_000);
    expect(fetchMock.mock.calls[0]?.[1]?.method).toBe("POST");
  }, 10_000);
});
