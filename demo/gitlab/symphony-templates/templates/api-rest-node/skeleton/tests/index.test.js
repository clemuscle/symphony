const request = require("supertest");
const app = require("../src/index");

describe("{{SERVICE_NAME}} API", () => {
  test("GET /healthz — retourne status ok", async () => {
    const res = await request(app).get("/healthz");
    expect(res.statusCode).toBe(200);
    expect(res.body.status).toBe("ok");
    expect(res.body.service).toBeDefined();
    expect(res.body.version).toBeDefined();
  });

  test("GET / — retourne le service running", async () => {
    const res = await request(app).get("/");
    expect(res.statusCode).toBe(200);
    expect(res.body.status).toBe("running");
  });

  test("GET /metrics — expose les métriques Prometheus", async () => {
    const res = await request(app).get("/metrics");
    expect(res.statusCode).toBe(200);
    expect(res.text).toContain("http_requests_total");
  });
});

