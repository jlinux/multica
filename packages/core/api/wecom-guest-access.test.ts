// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "./client";

const response = {
  enabled: true,
  allowed_group_ids: ["group-a"],
  allow_direct_messages: false,
  version: "env:one",
  source: "environment",
  sponsor_user_id: "owner-1",
  updated_by: null,
  updated_at: null,
  groups: [{ chat_id: "group-a", name: null }],
};
function mockResponse(body: unknown, status = 200) {
  const mock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), {
    status, headers: { "Content-Type": "application/json" },
  }));
  vi.stubGlobal("fetch", mock);
  return mock;
}
afterEach(() => vi.unstubAllGlobals());

describe("WeCom guest access API", () => {
  it("reads the scoped environment policy and converts wire fields", async () => {
    const fetch = mockResponse({ ...response, future_field: true });
    const result = await new ApiClient("https://api.example.test").getWecomGuestAccess("ws-1", "install-1");
    expect(fetch.mock.calls[0]?.[0]).toBe("https://api.example.test/api/workspaces/ws-1/wecom/installations/install-1/guest-access");
    expect(result).toMatchObject({ enabled: true, allowedGroupIds: ["group-a"], allowDirectMessages: false,
      version: "env:one", source: "environment", sponsorUserId: "owner-1", updatedBy: null,
      groups: [{ chatId: "group-a", name: null }] });
  });

  it.each([null, {}, { ...response, enabled: "yes" }, { ...response, version: "" },
    { ...response, allowed_group_ids: [1] }, { ...response, groups: [{ chat_id: 3 }] },
  ])("rejects malformed loads instead of returning writable defaults: %j", async (body) => {
    mockResponse(body);
    await expect(new ApiClient("https://api.example.test").getWecomGuestAccess("ws-1", "install-1"))
      .rejects.toThrow(/invalid.*guest access/i);
  });

  it("writes only policy fields and version, with no selectable sponsor", async () => {
    const fetch = mockResponse({ ...response, source: "database", version: "db:two" });
    const result = await new ApiClient("https://api.example.test").updateWecomGuestAccess("ws-1", "install-1", {
      enabled: false, allowedGroupIds: ["group-b"], allowDirectMessages: true, version: "env:one",
    });
    expect(fetch.mock.calls[0]?.[1]?.method).toBe("PUT");
    expect(JSON.parse(fetch.mock.calls[0]?.[1]?.body)).toEqual({
      enabled: false, allowed_group_ids: ["group-b"], allow_direct_messages: true, version: "env:one",
    });
    expect(result.version).toBe("db:two");
  });

  it("rejects a malformed successful save response", async () => {
    mockResponse({});
    await expect(new ApiClient("https://api.example.test").updateWecomGuestAccess("ws-1", "install-1", {
      enabled: false, allowedGroupIds: [], allowDirectMessages: false, version: "v1",
    })).rejects.toThrow(/invalid.*guest access/i);
  });

  it("preserves a conflict status for the dialog", async () => {
    mockResponse({ error: "configuration changed" }, 409);
    await expect(new ApiClient("https://api.example.test").updateWecomGuestAccess("ws-1", "install-1", {
      enabled: false, allowedGroupIds: [], allowDirectMessages: false, version: "v1",
    })).rejects.toMatchObject({ status: 409 });
  });

  it("converts the member-visible summary at the API boundary", async () => {
    mockResponse({ installations: [{ id: "i1", guest_access: { status: "enabled", allowed_group_count: 4, allow_direct_messages: false } }], configured: true });
    const result = await new ApiClient("https://api.example.test").listWecomInstallations("ws-1");
    expect(result.installations[0]?.guestAccess).toEqual({ status: "enabled", allowedGroupCount: 4, allowDirectMessages: false });
  });

  it("retains installations if the optional summary is malformed", async () => {
    mockResponse({ installations: [{ id: "i1", guest_access: { status: "enabled", allowed_group_count: "bad" } }], configured: true });
    const result = await new ApiClient("https://api.example.test").listWecomInstallations("ws-1");
    expect(result.installations).toHaveLength(1);
    expect(result.installations[0]?.guestAccess?.status).toBe("unavailable");
  });
});
