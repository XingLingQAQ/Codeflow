#!/usr/bin/env node
/**
 * Sanity: experimental backend route handlers must remain registered in router.go
 */
import fs from "fs";
import path from "path";
import { fileURLToPath } from "url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const router = fs.readFileSync(path.join(root, "backend/internal/api/router.go"), "utf8");

const required = [
  'Group("/flows")',
  'Group("/workspace")',
  'Group("/skills")',
  'Group("/guard")',
  "handlers.CreateFlow",
  "handlers.ListFlowStages",
  "handlers.ListFlowArtifacts",
  "handlers.AttachFlowArtifact",
  "handlers.UpdateFlowArtifactStatus",
  "handlers.WriteWorkspaceFile",
  "handlers.PromoteWorkspaceFile",
  "handlers.CreateSkill",
  "handlers.ImportSkills",
  "handlers.GuardCheck",
  "handlers.GuardIndexTree",
  "handlers.GuardExempt",
  "handlers.GuardListExemptions",
  "handlers.GuardClearExemption",
  "handlers.DeleteFlow",
  "handlers.AbortFlow",
];

let failed = false;
for (const needle of required) {
  if (!router.includes(needle)) {
    console.error(`[check-backend-routes] MISSING: ${needle}`);
    failed = true;
  }
}
if (failed) process.exit(1);
console.log(`[check-backend-routes] OK (${required.length} markers)`);
