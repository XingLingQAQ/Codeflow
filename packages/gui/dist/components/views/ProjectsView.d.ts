/**
 * ProjectsView - 项目管理视图
 * 项目卡片网格布局、状态徽章、进度条、搜索和筛选
 */
import React from 'react';
export type ProjectStatus = 'active' | 'completed' | 'paused' | 'planning';
export interface Project {
    id: string;
    name: string;
    description: string;
    status: ProjectStatus;
    progress: number;
    members: Array<{
        name: string;
        src?: string;
    }>;
    updatedAt: Date;
    tags?: string[];
}
export interface ProjectsViewProps {
    projects?: Project[];
    onSelectProject?: (projectId: string) => void;
    onCreateProject?: () => void;
    className?: string;
    style?: React.CSSProperties;
}
export declare const ProjectsView: React.FC<ProjectsViewProps>;
export default ProjectsView;
//# sourceMappingURL=ProjectsView.d.ts.map