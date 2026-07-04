const express = require("express");
const app = express();
app.use(express.json());

app.get("/healthz", (req, res) => res.json({ status: "ok", service: "{{.ServiceName}}" }));
app.get("/metrics", (req, res) => {
  res.set("Content-Type", "text/plain");
  res.send("# HELP http_requests_total Total requests\nhttp_requests_total 0\n");
});
app.get("/", (req, res) => res.json({ service: "{{.ServiceName}}", status: "running" }));

const PORT = process.env.PORT || {{.Port}};
app.listen(PORT, () => console.log(JSON.stringify({ msg: "started", port: PORT })));
module.exports = app;
