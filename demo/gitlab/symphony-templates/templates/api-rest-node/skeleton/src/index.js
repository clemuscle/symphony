/**
 * {{SERVICE_NAME}} — API REST Node.js
 */
const express = require("express");
const winston = require("winston");

const SERVICE_NAME = "{{SERVICE_NAME}}";
const VERSION = process.env.VERSION || "dev";
const PORT = process.env.PORT || 8080;

// Logger structuré JSON
const logger = winston.createLogger({
  level: "info",
  format: winston.format.combine(
    winston.format.timestamp(),
    winston.format.json()
  ),
  transports: [new winston.transports.Console()],
});

const app = express();
app.use(express.json());

// Logging middleware
app.use((req, res, next) => {
  logger.info({ method: req.method, path: req.path, ip: req.ip });
  next();
});

// Healthcheck
app.get("/healthz", (req, res) => {
  res.json({ status: "ok", service: SERVICE_NAME, version: VERSION });
});

// Métriques Prometheus
app.get("/metrics", (req, res) => {
  res.set("Content-Type", "text/plain");
  res.send(
    "# HELP http_requests_total Total HTTP requests\n" +
    "# TYPE http_requests_total counter\n" +
    "http_requests_total 0\n"
  );
});

// Route principale
app.get("/", (req, res) => {
  res.json({ service: SERVICE_NAME, status: "running", version: VERSION });
});

// Graceful shutdown
process.on("SIGTERM", () => {
  logger.info("SIGTERM received, shutting down gracefully");
  process.exit(0);
});

app.listen(PORT, () => {
  logger.info({ msg: `${SERVICE_NAME} started`, port: PORT, version: VERSION });
});

module.exports = app;

