#!/usr/bin/env node
/**
 * CodeFlow CLI 入口
 */

import { Command } from 'commander';
import { registerShadowCommand } from './commands/shadow.js';

const program = new Command();

program
  .name('codeflow')
  .description('CodeFlow CLI - 新一代智能集成开发环境')
  .version('0.1.0');

registerShadowCommand(program);

program.parse();
