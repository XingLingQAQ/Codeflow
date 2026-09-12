import { test } from 'node:test';
import assert from 'node:assert/strict';
import { greet } from '../src/greet.ts';

test('greet returns the greeting for a name', () => {
  assert.equal(greet('Codeflow'), 'Hello, Codeflow!');
});

test('greet handles an empty name', () => {
  assert.equal(greet(''), 'Hello, !');
});
