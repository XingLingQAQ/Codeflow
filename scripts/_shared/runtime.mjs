import fs from 'fs';
import path from 'path';
import { fileURLToPath, pathToFileURL } from 'url';

const sharedDir = path.dirname(fileURLToPath(import.meta.url));
export const scriptsDir = path.resolve(sharedDir, '..');
export const repoRoot = path.resolve(scriptsDir, '..');
export const artifactsDir = path.resolve(repoRoot, 'artifacts');

export function ensureArtifactsDir() {
  fs.mkdirSync(artifactsDir, { recursive: true });
  return artifactsDir;
}

export async function importCoreDist(relativePath) {
  const absolutePath = path.resolve(repoRoot, 'packages', 'core', 'dist', relativePath);
  if (!fs.existsSync(absolutePath)) {
    throw new Error(
      `Missing dist file: ${absolutePath}. Run "pnpm --filter @codeflow/core build" first.`
    );
  }
  return import(pathToFileURL(absolutePath).href);
}

export function getEnv(name, fallback = '') {
  const value = process.env[name];
  if (typeof value !== 'string') return fallback;
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : fallback;
}

export function getBooleanEnv(name, fallback) {
  const value = process.env[name];
  if (typeof value !== 'string') return fallback;
  const normalized = value.trim().toLowerCase();
  if (['1', 'true', 'yes', 'on'].includes(normalized)) return true;
  if (['0', 'false', 'no', 'off'].includes(normalized)) return false;
  return fallback;
}
