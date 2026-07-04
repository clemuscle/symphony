const request = require("supertest");
const app = require("../src/index");

test("GET /healthz", async () => {
  const r = await request(app).get("/healthz");
  expect(r.statusCode).toBe(200);
  expect(r.body.status).toBe("ok");
});

test("GET /metrics", async () => {
  const r = await request(app).get("/metrics");
  expect(r.statusCode).toBe(200);
  expect(r.text).toContain("http_requests_total");
});
