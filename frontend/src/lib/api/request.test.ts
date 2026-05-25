/**
 * Seed Vitest spec for the shared API request wrapper. Pins the two
 * branches the rest of the UI depends on:
 *
 *   1. 2xx JSON → parsed body.
 *   2. 4xx with ErrorResponse → ApiError (or StateMovedError for 409
 *      VERSION_MISMATCH) carrying code / status / details / requestId.
 *
 * Mocks `fetch` directly (no MSW yet) — the helper is small enough that
 * a hand-rolled stub is clearer than a network mocking layer. When the
 * frontend grows its test corpus, swap to MSW.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// Mock the connection signal bus BEFORE importing request.ts. The
// request helper calls `emitSignal("degraded")` on 503 DEGRADED_MODE;
// the suite below pins that contract.
vi.mock("@/lib/connection/signals", () => ({
  emitSignal: vi.fn(),
}));

import { emitSignal } from "@/lib/connection/signals";
import { ApiError, StateMovedError, request } from "./request";

type FetchFn = typeof fetch;

function mockFetchOnce(response: Response) {
  const fn = vi.fn<FetchFn>().mockResolvedValueOnce(response);
  vi.stubGlobal("fetch", fn);
  return fn;
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.mocked(emitSignal).mockClear();
});

describe("request()", () => {
  it("parses JSON on a 200 response", async () => {
    const body = { id: "run-1", status: "RUNNING" as const };
    mockFetchOnce(
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const out = await request<typeof body>("/api/runs/run-1");
    expect(out).toEqual(body);
  });

  it("returns undefined on a 204 response without parsing", async () => {
    mockFetchOnce(new Response(null, { status: 204 }));
    const out = await request<void>("/api/admin/restart", { method: "POST" });
    expect(out).toBeUndefined();
  });

  it("sends body + Content-Type for POST mutations", async () => {
    const fetchFn = mockFetchOnce(new Response("null", { status: 200 }));
    await request<unknown>("/api/runs", {
      method: "POST",
      body: { id: "run-2" },
    });

    expect(fetchFn).toHaveBeenCalledTimes(1);
    const [, init] = fetchFn.mock.calls[0]!;
    expect(init?.method).toBe("POST");
    expect(init?.body).toBe(JSON.stringify({ id: "run-2" }));
    const headers = init?.headers as Record<string, string>;
    expect(headers["Content-Type"]).toBe("application/json");
    expect(headers.Accept).toBe("application/json");
  });

  it("maps an ErrorResponse 400 to ApiError with code + status + details", async () => {
    const errBody = {
      error: {
        code: "INVALID_DAG",
        message: "cycle detected",
        request_id: "req-abc",
        details: { cycle: ["j1", "j2", "j1"] },
      },
    };
    mockFetchOnce(
      new Response(JSON.stringify(errBody), {
        status: 400,
        headers: { "Content-Type": "application/json" },
      }),
    );

    await expect(request("/api/runs", { method: "POST" })).rejects.toMatchObject({
      name: "ApiError",
      code: "INVALID_DAG",
      status: 400,
      message: "cycle detected",
      requestId: "req-abc",
      details: { cycle: ["j1", "j2", "j1"] },
    });
  });

  it("maps a 409 VERSION_MISMATCH to StateMovedError with currentVersion", async () => {
    const errBody = {
      error: {
        code: "VERSION_MISMATCH",
        message: "state moved",
        details: { current_version: 7 },
      },
    };
    mockFetchOnce(
      new Response(JSON.stringify(errBody), {
        status: 409,
        headers: { "Content-Type": "application/json" },
      }),
    );

    const err = await request("/api/runs/run-1/cancel", { method: "POST" }).catch(
      (e: unknown) => e,
    );
    expect(err).toBeInstanceOf(StateMovedError);
    expect(err).not.toBeInstanceOf(ApiError);
    if (err instanceof StateMovedError) {
      expect(err.code).toBe("STATE_MOVED");
      expect(err.currentVersion).toBe(7);
    }
  });

  it("maps a fetch-level rejection (offline / DNS) to ApiError NETWORK", async () => {
    const fn = vi.fn<FetchFn>().mockRejectedValueOnce(new TypeError("Failed to fetch"));
    vi.stubGlobal("fetch", fn);

    const err = await request("/api/runs").catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ApiError);
    if (err instanceof ApiError) {
      expect(err.code).toBe("UNKNOWN");
      expect(err.status).toBe(0);
      expect(err.details).toEqual({ kind: "NETWORK" });
      expect(err.message).toBe("Failed to fetch");
    }
    expect(emitSignal).not.toHaveBeenCalled();
  });

  it("falls back to UNKNOWN code when error body isn't JSON", async () => {
    mockFetchOnce(
      new Response("Internal Server Error", {
        status: 500,
        statusText: "Internal Server Error",
      }),
    );

    await expect(request("/api/runs")).rejects.toMatchObject({
      name: "ApiError",
      code: "UNKNOWN",
      status: 500,
    });
  });
});

describe("connection signal emission", () => {
  it("emits 'degraded' on 503 with code DEGRADED_MODE", async () => {
    const errBody = {
      error: {
        code: "DEGRADED_MODE",
        message: "Orchestrator reconciling; mutations temporarily refused.",
      },
    };
    mockFetchOnce(
      new Response(JSON.stringify(errBody), {
        status: 503,
        headers: { "Content-Type": "application/json" },
      }),
    );

    await expect(request("/api/runs/run-1/cancel", { method: "POST" })).rejects.toMatchObject({
      name: "ApiError",
      code: "DEGRADED_MODE",
      status: 503,
    });

    expect(emitSignal).toHaveBeenCalledTimes(1);
    expect(emitSignal).toHaveBeenCalledWith("degraded");
  });

  it("does NOT emit 'degraded' on unrelated 4xx errors", async () => {
    mockFetchOnce(
      new Response(JSON.stringify({ error: { code: "RUN_NOT_FOUND", message: "no" } }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      }),
    );
    await expect(request("/api/runs/missing")).rejects.toMatchObject({ code: "RUN_NOT_FOUND" });
    expect(emitSignal).not.toHaveBeenCalled();
  });

  it("does NOT emit on a successful response", async () => {
    mockFetchOnce(new Response("null", { status: 200 }));
    await request<unknown>("/api/runs");
    expect(emitSignal).not.toHaveBeenCalled();
  });
});

describe("ApiError", () => {
  it("carries all opts onto the instance", () => {
    const err = new ApiError("boom", {
      code: "EXECUTOR_UNREACHABLE",
      status: 423,
      details: { deck_id: "deck-2" },
      requestId: "req-xyz",
    });
    expect(err.name).toBe("ApiError");
    expect(err.message).toBe("boom");
    expect(err.code).toBe("EXECUTOR_UNREACHABLE");
    expect(err.status).toBe(423);
    expect(err.details).toEqual({ deck_id: "deck-2" });
    expect(err.requestId).toBe("req-xyz");
  });
});

describe("setup uses fetch stub", () => {
  let fetchFn: ReturnType<typeof vi.fn<FetchFn>>;

  beforeEach(() => {
    fetchFn = vi.fn<FetchFn>().mockResolvedValue(new Response("null", { status: 200 }));
    vi.stubGlobal("fetch", fetchFn);
  });

  it("forwards Accept override", async () => {
    await request<unknown>("/api/runs/run-1", { accept: "text/event-stream" });
    const [, init] = fetchFn.mock.calls[0]!;
    const headers = init?.headers as Record<string, string>;
    expect(headers.Accept).toBe("text/event-stream");
  });
});
