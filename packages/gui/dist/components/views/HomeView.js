import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * HomeView - 首页视图
 * 渐变背景 + 浮动动画、模型选择器、大型输入框 + 快捷操作
 */
import { useState, useEffect, useCallback } from 'react';
import { colors, spacing, borderRadius, fontSize, fontWeight, shadows, transitions, keyframes, animations, breakpoints, } from '../shared/tokens';
import { CustomSelect } from '../shared/CustomSelect';
import { Button } from '../shared/Button';
// Inject keyframes for floating animation
const injectKeyframes = () => {
    const styleId = 'home-view-keyframes';
    if (typeof document !== 'undefined' && !document.getElementById(styleId)) {
        const style = document.createElement('style');
        style.id = styleId;
        style.textContent = keyframes.float + keyframes.pulseSlow;
        document.head.appendChild(style);
    }
};
// Model options
const modelOptions = [
    { value: 'claude-opus', label: 'Claude Opus 4.5' },
    { value: 'claude-sonnet', label: 'Claude Sonnet 4' },
    { value: 'gpt-4o', label: 'GPT-4o' },
    { value: 'gemini-pro', label: 'Gemini 2.0 Pro' },
];
// Quick action suggestions
const quickActions = [
    { icon: '💡', label: 'Explain code', prompt: 'Explain this code: ' },
    { icon: '🐛', label: 'Debug issue', prompt: 'Help me debug: ' },
    { icon: '✨', label: 'Refactor', prompt: 'Refactor this code: ' },
    { icon: '📝', label: 'Write tests', prompt: 'Write tests for: ' },
    { icon: '📖', label: 'Documentation', prompt: 'Generate documentation for: ' },
    { icon: '🔍', label: 'Code review', prompt: 'Review this code: ' },
];
// Terminal icon
const TerminalIcon = () => (_jsxs("svg", { width: "24", height: "24", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("polyline", { points: "4 17 10 11 4 5" }), _jsx("line", { x1: "12", y1: "19", x2: "20", y2: "19" })] }));
const SendIcon = () => (_jsxs("svg", { width: "20", height: "20", viewBox: "0 0 24 24", fill: "none", stroke: "currentColor", strokeWidth: "2", strokeLinecap: "round", strokeLinejoin: "round", children: [_jsx("line", { x1: "22", y1: "2", x2: "11", y2: "13" }), _jsx("polygon", { points: "22 2 15 22 11 13 2 9 22 2" })] }));
export const HomeView = ({ onStartSession, className, style, }) => {
    const [prompt, setPrompt] = useState('');
    const [selectedModel, setSelectedModel] = useState('claude-opus');
    const [isMobile, setIsMobile] = useState(false);
    useEffect(() => {
        injectKeyframes();
        const checkMobile = () => setIsMobile(window.innerWidth < breakpoints.md);
        checkMobile();
        window.addEventListener('resize', checkMobile);
        return () => window.removeEventListener('resize', checkMobile);
    }, []);
    const handleSubmit = useCallback(() => {
        if (prompt.trim() && onStartSession) {
            onStartSession(prompt.trim(), selectedModel);
            setPrompt('');
        }
    }, [prompt, selectedModel, onStartSession]);
    const handleKeyDown = useCallback((e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
            e.preventDefault();
            handleSubmit();
        }
    }, [handleSubmit]);
    const handleQuickAction = useCallback((actionPrompt) => {
        setPrompt(actionPrompt);
    }, []);
    return (_jsxs("div", { className: className, style: {
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'center',
            justifyContent: 'center',
            minHeight: '100%',
            padding: isMobile ? spacing[4] : spacing[8],
            background: `linear-gradient(135deg, ${colors.slate[50]} 0%, ${colors.primary[50]} 50%, ${colors.indigo[50]} 100%)`,
            position: 'relative',
            overflow: 'hidden',
            ...style,
        }, children: [_jsx("div", { style: {
                    position: 'absolute',
                    top: '10%',
                    left: '10%',
                    width: 200,
                    height: 200,
                    borderRadius: borderRadius.full,
                    background: `radial-gradient(circle, ${colors.primary[200]}40 0%, transparent 70%)`,
                    animation: animations.float,
                    pointerEvents: 'none',
                } }), _jsx("div", { style: {
                    position: 'absolute',
                    bottom: '20%',
                    right: '15%',
                    width: 150,
                    height: 150,
                    borderRadius: borderRadius.full,
                    background: `radial-gradient(circle, ${colors.indigo[200]}40 0%, transparent 70%)`,
                    animation: animations.float,
                    animationDelay: '1s',
                    pointerEvents: 'none',
                } }), _jsxs("div", { style: {
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    maxWidth: 640,
                    width: '100%',
                    zIndex: 1,
                }, children: [_jsxs("div", { style: {
                            display: 'flex',
                            alignItems: 'center',
                            gap: spacing[3],
                            marginBottom: spacing[6],
                        }, children: [_jsx("div", { style: {
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    width: 48,
                                    height: 48,
                                    backgroundColor: colors.slate[900],
                                    borderRadius: borderRadius.xl,
                                    color: '#fff',
                                    boxShadow: shadows.lg,
                                }, children: _jsx(TerminalIcon, {}) }), _jsx("h1", { style: {
                                    fontSize: isMobile ? fontSize['2xl'] : fontSize['4xl'],
                                    fontWeight: fontWeight.bold,
                                    color: colors.slate[800],
                                    letterSpacing: '-0.025em',
                                }, children: "CodeFlow" })] }), _jsx("p", { style: {
                            fontSize: isMobile ? fontSize.base : fontSize.lg,
                            color: colors.slate[500],
                            textAlign: 'center',
                            marginBottom: spacing[8],
                            maxWidth: 400,
                        }, children: "Your AI-powered coding assistant. Ask anything about code." }), _jsx("div", { style: {
                            display: 'flex',
                            gap: spacing[2],
                            marginBottom: spacing[4],
                            flexWrap: 'wrap',
                            justifyContent: 'center',
                        }, children: _jsx(CustomSelect, { options: modelOptions, value: selectedModel, onChange: setSelectedModel, placeholder: "Select model", style: { minWidth: 180 } }) }), _jsxs("div", { style: {
                            width: '100%',
                            backgroundColor: '#fff',
                            borderRadius: borderRadius['2xl'],
                            boxShadow: shadows.xl,
                            padding: spacing[4],
                            marginBottom: spacing[6],
                        }, children: [_jsx("textarea", { value: prompt, onChange: (e) => setPrompt(e.target.value), onKeyDown: handleKeyDown, placeholder: "Ask me anything about code...", style: {
                                    width: '100%',
                                    minHeight: 120,
                                    padding: spacing[3],
                                    fontSize: fontSize.base,
                                    fontFamily: 'inherit',
                                    border: `1px solid ${colors.slate[200]}`,
                                    borderRadius: borderRadius.xl,
                                    backgroundColor: colors.slate[50],
                                    resize: 'vertical',
                                    outline: 'none',
                                    transition: transitions.fast,
                                }, onFocus: (e) => {
                                    e.target.style.borderColor = colors.primary[400];
                                    e.target.style.backgroundColor = '#fff';
                                    e.target.style.boxShadow = `0 0 0 3px ${colors.primary[100]}`;
                                }, onBlur: (e) => {
                                    e.target.style.borderColor = colors.slate[200];
                                    e.target.style.backgroundColor = colors.slate[50];
                                    e.target.style.boxShadow = 'none';
                                } }), _jsx("div", { style: {
                                    display: 'flex',
                                    justifyContent: 'flex-end',
                                    marginTop: spacing[3],
                                }, children: _jsxs(Button, { variant: "primary", onClick: handleSubmit, disabled: !prompt.trim(), style: {
                                        display: 'flex',
                                        alignItems: 'center',
                                        gap: spacing[2],
                                    }, children: [_jsx(SendIcon, {}), _jsx("span", { children: "Send" })] }) })] }), _jsx("div", { style: {
                            display: 'grid',
                            gridTemplateColumns: isMobile ? 'repeat(2, 1fr)' : 'repeat(3, 1fr)',
                            gap: spacing[3],
                            width: '100%',
                        }, children: quickActions.map((action, index) => (_jsxs("button", { onClick: () => handleQuickAction(action.prompt), style: {
                                display: 'flex',
                                alignItems: 'center',
                                gap: spacing[2],
                                padding: `${spacing[3]}px ${spacing[4]}px`,
                                backgroundColor: '#fff',
                                border: `1px solid ${colors.slate[200]}`,
                                borderRadius: borderRadius.xl,
                                cursor: 'pointer',
                                transition: transitions.fast,
                                fontSize: fontSize.sm,
                                color: colors.slate[600],
                                fontWeight: fontWeight.medium,
                            }, onMouseEnter: (e) => {
                                e.currentTarget.style.borderColor = colors.primary[300];
                                e.currentTarget.style.boxShadow = shadows.md;
                                e.currentTarget.style.transform = 'translateY(-2px)';
                            }, onMouseLeave: (e) => {
                                e.currentTarget.style.borderColor = colors.slate[200];
                                e.currentTarget.style.boxShadow = 'none';
                                e.currentTarget.style.transform = 'translateY(0)';
                            }, children: [_jsx("span", { style: { fontSize: fontSize.lg }, children: action.icon }), _jsx("span", { children: action.label })] }, index))) })] })] }));
};
export default HomeView;
//# sourceMappingURL=HomeView.js.map