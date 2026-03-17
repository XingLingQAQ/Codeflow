import type { Diff, EditResult } from '../cowork/types.js';
import type { FileApplyDiffInput, FileEditInput, FileEditMultipleInput, FilePreviewInput, FileUndoResult, ToolContext } from './types.js';
/**
 * FileOperationService
 * 统一承接 preview/edit/applyDiff/undo 文件工具能力
 */
export declare class FileOperationService {
    preview(input: FilePreviewInput, context: ToolContext): Promise<Diff>;
    edit(input: FileEditInput, context: ToolContext): Promise<EditResult>;
    editMultiple(input: FileEditMultipleInput, context: ToolContext): Promise<EditResult[]>;
    applyDiff(input: FileApplyDiffInput, context: ToolContext): Promise<{
        applied: true;
    }>;
    undo(_input: Record<string, never>, context: ToolContext): Promise<FileUndoResult>;
}
//# sourceMappingURL=FileOperationService.d.ts.map