#!/usr/bin/env node
/**
 * Ensure experimental OpenAPI paths for flows/workspace/skills/guard exist
 * for every method registered in router.go experimental groups.
 */
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const router = fs.readFileSync(path.join(root, "backend/internal/api/router.go"), "utf8");
const openapi = fs.readFileSync(path.join(root, "backend/docs/openapi.yaml"), "utf8");

// Parse openapi path+method set
const ops = new Set();
let cur = "";
for (const line of openapi.split(/\r?\n/)) {
  const pm = line.match(/^  (\/api\/v1\/[^:]+):\s*$/);
  if (pm) {
    cur = pm[1];
    continue;
  }
  const mm = line.match(/^    (get|post|put|patch|delete):\s*$/);
  if (cur && mm) ops.add(mm[1].toUpperCase() + " " + cur);
}

// Experimental surface we care about
const prefixes = ["/api/v1/flows", "/api/v1/workspace", "/api/v1/skills", "/api/v1/guard"];
const required = [
  ["GET", "/api/v1/flows/templates"],
  ["GET", "/api/v1/flows/templates/{tid}"],
  ["GET", "/api/v1/flows"],
  ["POST", "/api/v1/flows"],
  ["GET", "/api/v1/flows/{id}"],
  ["DELETE", "/api/v1/flows/{id}"],
  ["GET", "/api/v1/flows/{id}/events"],
  ["GET", "/api/v1/flows/{id}/active-stage"],
  ["GET", "/api/v1/flows/{id}/gates"],
  ["GET", "/api/v1/flows/{id}/stages"],
  ["GET", "/api/v1/flows/{id}/stages/{sid}"],
  ["GET", "/api/v1/flows/{id}/artifacts"],
  ["GET", "/api/v1/flows/{id}/artifacts/{aid}"],
  ["PATCH", "/api/v1/flows/{id}/artifacts/{aid}"],
  ["POST", "/api/v1/flows/{id}/stages/{sid}/advance"],
  ["POST", "/api/v1/flows/{id}/stages/{sid}/skip"],
  ["POST", "/api/v1/flows/{id}/stages/{sid}/artifacts"],
  ["POST", "/api/v1/flows/{id}/loop"],
  ["POST", "/api/v1/flows/{id}/abort"],
  ["POST", "/api/v1/flows/{id}/gates/{gid}/decide"],
  ["GET", "/api/v1/workspace/list"],
  ["GET", "/api/v1/workspace/read"],
  ["GET", "/api/v1/workspace/stat"],
  ["GET", "/api/v1/workspace/staged"],
  ["POST", "/api/v1/workspace/write"],
  ["POST", "/api/v1/workspace/promote"],
  ["POST", "/api/v1/workspace/promote-all"],
  ["POST", "/api/v1/workspace/discard"],
  ["POST", "/api/v1/workspace/discard-all"],
  ["POST", "/api/v1/skills"],
  ["GET", "/api/v1/skills"],
  ["POST", "/api/v1/skills/match"],
  ["POST", "/api/v1/skills/inject"],
  ["POST", "/api/v1/skills/import"],
  ["GET", "/api/v1/skills/export"],
  ["GET", "/api/v1/skills/{id}"],
  ["PATCH", "/api/v1/skills/{id}"],
  ["DELETE", "/api/v1/skills/{id}"],
  ["POST", "/api/v1/guard/check"],
  ["GET", "/api/v1/guard/config"],
  ["GET", "/api/v1/guard/rules"],
  ["POST", "/api/v1/guard/index"],
  ["POST", "/api/v1/guard/exempt"],
  ["GET", "/api/v1/guard/exemptions"],
  ["DELETE", "/api/v1/guard/exempt"],
];

// Sanity: experimental handlers still in router
const handlerMarkers = [
  "handlers.ListFlowTemplates",
  "handlers.WriteWorkspaceFile",
  "handlers.CreateSkill",
  "handlers.GuardCheck",
  "handlers.GetHookEvents",
];
let failed = false;
for (const m of handlerMarkers) {
  if (!router.includes(m)) {
    console.error("[check-experimental-openapi] router missing", m);
    failed = true;
  }
}
for (const [method, p] of required) {
  const key = method + " " + p;
  if (!ops.has(key)) {
    console.error("[check-experimental-openapi] OpenAPI missing", key);
    failed = true;
  }
}
if (failed) process.exit(1);
console.log(`[check-experimental-openapi] OK (${required.length} experimental ops)`);
