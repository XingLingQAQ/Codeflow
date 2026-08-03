/**
 * Minimal line-level diff (LCS based) for the workbench staging/diff views.
 * Good enough for staged-file previews; Monaco's diff editor can replace the
 * rendering later without changing call sites.
 */

export interface DiffOp {
  type: 'same' | 'add' | 'del';
  text: string;
  /** 1-based line number in the base (old) text; undefined for adds. */
  aLine?: number;
  /** 1-based line number in the current (new) text; undefined for dels. */
  bLine?: number;
}

export interface DiffStats {
  adds: number;
  dels: number;
}

const MAX_DP_CELLS = 400_000;

/** Line diff between `base` (old) and `current` (new). */
export function diffLines(base: string, current: string): DiffOp[] {
  const a = base.split('\n');
  const b = current.split('\n');

  // Trim common prefix / suffix so the DP only sees the changed middle.
  let start = 0;
  while (start < a.length && start < b.length && a[start] === b[start]) start++;
  let endA = a.length;
  let endB = b.length;
  while (endA > start && endB > start && a[endA - 1] === b[endB - 1]) {
    endA--;
    endB--;
  }

  const ops: DiffOp[] = [];
  let aLine = 1;
  let bLine = 1;
  for (let i = 0; i < start; i++) {
    ops.push({ type: 'same', text: a[i], aLine: aLine++, bLine: bLine++ });
  }

  const midA = a.slice(start, endA);
  const midB = b.slice(start, endB);

  if (midA.length > 0 || midB.length > 0) {
    if ((midA.length + 1) * (midB.length + 1) > MAX_DP_CELLS) {
      // Too large for the DP table — degrade to whole-block replace.
      for (const text of midA) ops.push({ type: 'del', text, aLine: aLine++ });
      for (const text of midB) ops.push({ type: 'add', text, bLine: bLine++ });
    } else {
      // LCS lengths table (rows: midA, cols: midB).
      const cols = midB.length + 1;
      const dp = new Int32Array((midA.length + 1) * cols);
      for (let i = midA.length - 1; i >= 0; i--) {
        for (let j = midB.length - 1; j >= 0; j--) {
          dp[i * cols + j] =
            midA[i] === midB[j]
              ? dp[(i + 1) * cols + j + 1] + 1
              : Math.max(dp[(i + 1) * cols + j], dp[i * cols + j + 1]);
        }
      }
      let i = 0;
      let j = 0;
      while (i < midA.length && j < midB.length) {
        if (midA[i] === midB[j]) {
          ops.push({ type: 'same', text: midA[i], aLine: aLine++, bLine: bLine++ });
          i++;
          j++;
        } else if (dp[(i + 1) * cols + j] >= dp[i * cols + j + 1]) {
          ops.push({ type: 'del', text: midA[i], aLine: aLine++ });
          i++;
        } else {
          ops.push({ type: 'add', text: midB[j], bLine: bLine++ });
          j++;
        }
      }
      while (i < midA.length) {
        ops.push({ type: 'del', text: midA[i], aLine: aLine++ });
        i++;
      }
      while (j < midB.length) {
        ops.push({ type: 'add', text: midB[j], bLine: bLine++ });
        j++;
      }
    }
  }

  for (let i = endA; i < a.length; i++) {
    ops.push({ type: 'same', text: a[i], aLine: aLine++, bLine: bLine++ });
  }
  return ops;
}

export function diffStats(base: string, current: string): DiffStats {
  const ops = diffLines(base, current);
  let adds = 0;
  let dels = 0;
  for (const op of ops) {
    if (op.type === 'add') adds++;
    else if (op.type === 'del') dels++;
  }
  return { adds, dels };
}
