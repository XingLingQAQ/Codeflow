import { jsx as _jsx, Fragment as _Fragment, jsxs as _jsxs } from "react/jsx-runtime";
/**
 * ModelSelector - 模型选择器组件
 * 支持搜索、筛选和选择模型
 */
import { useState, useRef, useEffect, useCallback, useMemo } from 'react';
import { PROVIDER_ICONS, formatCost, } from './types';
import { ModelCard } from './ModelCard';
import { ModelFilter } from './ModelFilter';
import { ModelSearch } from './ModelSearch';
export const ModelSelector = ({ models, selectedModelId, onSelect, disabled = false, loading = false, placeholder = 'Select a model', className, style, showCost = true, showCapabilities = true, maxCapabilitiesToShow = 3, }) => {
    const [isOpen, setIsOpen] = useState(false);
    const [searchQuery, setSearchQuery] = useState('');
    const [selectedProvider, setSelectedProvider] = useState();
    const [selectedCapabilities, setSelectedCapabilities] = useState([]);
    const [showFilters, setShowFilters] = useState(false);
    const containerRef = useRef(null);
    const selectedModel = models.find(m => m.id === selectedModelId);
    // 获取可用的提供商和能力
    const availableProviders = useMemo(() => {
        const providers = new Set();
        models.forEach(m => providers.add(m.provider));
        return Array.from(providers);
    }, [models]);
    const availableCapabilities = useMemo(() => {
        const caps = new Set();
        models.forEach(m => m.capabilities.forEach(c => caps.add(c)));
        return Array.from(caps);
    }, [models]);
    // 筛选模型
    const filteredModels = useMemo(() => {
        return models.filter(model => {
            // 搜索过滤
            if (searchQuery) {
                const query = searchQuery.toLowerCase();
                const matchesName = model.name.toLowerCase().includes(query);
                const matchesProvider = model.provider.toLowerCase().includes(query);
                const matchesDescription = model.description?.toLowerCase().includes(query);
                if (!matchesName && !matchesProvider && !matchesDescription) {
                    return false;
                }
            }
            // 提供商过滤
            if (selectedProvider && model.provider !== selectedProvider) {
                return false;
            }
            // 能力过滤
            if (selectedCapabilities.length > 0) {
                const hasAllCaps = selectedCapabilities.every(cap => model.capabilities.includes(cap));
                if (!hasAllCaps) {
                    return false;
                }
            }
            return true;
        });
    }, [models, searchQuery, selectedProvider, selectedCapabilities]);
    // 点击外部关闭
    useEffect(() => {
        const handleClickOutside = (event) => {
            if (containerRef.current &&
                !containerRef.current.contains(event.target)) {
                setIsOpen(false);
            }
        };
        document.addEventListener('mousedown', handleClickOutside);
        return () => document.removeEventListener('mousedown', handleClickOutside);
    }, []);
    const handleSelect = useCallback((modelId) => {
        onSelect(modelId);
        setIsOpen(false);
    }, [onSelect]);
    const handleKeyDown = useCallback((event) => {
        if (event.key === 'Escape') {
            setIsOpen(false);
        }
        else if (event.key === 'Enter' || event.key === ' ') {
            if (!disabled) {
                setIsOpen(!isOpen);
            }
        }
    }, [isOpen, disabled]);
    const hasActiveFilters = selectedProvider || selectedCapabilities.length > 0;
    return (_jsxs("div", { ref: containerRef, className: className, style: {
            position: 'relative',
            display: 'inline-block',
            ...style,
        }, children: [_jsxs("button", { onClick: () => !disabled && setIsOpen(!isOpen), onKeyDown: handleKeyDown, disabled: disabled, style: {
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    padding: '8px 12px',
                    backgroundColor: '#fff',
                    border: '1px solid #ddd',
                    borderRadius: 6,
                    cursor: disabled ? 'not-allowed' : 'pointer',
                    opacity: disabled ? 0.6 : 1,
                    minWidth: 220,
                    fontSize: 13,
                    textAlign: 'left',
                }, children: [loading ? (_jsx("span", { style: { color: '#666' }, children: "Loading..." })) : selectedModel ? (_jsxs(_Fragment, { children: [_jsx("span", { children: PROVIDER_ICONS[selectedModel.provider] }), _jsx("span", { style: { flex: 1 }, children: selectedModel.name }), showCost && (_jsx("span", { style: { fontSize: 11, color: '#666' }, children: formatCost(selectedModel.cost) }))] })) : (_jsx("span", { style: { color: '#999', flex: 1 }, children: placeholder })), _jsx("span", { style: {
                            marginLeft: 'auto',
                            transform: isOpen ? 'rotate(180deg)' : 'rotate(0deg)',
                            transition: 'transform 0.2s',
                            fontSize: 10,
                        }, children: "\u25BC" })] }), isOpen && (_jsxs("div", { style: {
                    position: 'absolute',
                    top: '100%',
                    left: 0,
                    right: 0,
                    marginTop: 4,
                    backgroundColor: '#fff',
                    border: '1px solid #ddd',
                    borderRadius: 8,
                    boxShadow: '0 4px 16px rgba(0, 0, 0, 0.12)',
                    zIndex: 1000,
                    minWidth: 320,
                    maxHeight: 480,
                    display: 'flex',
                    flexDirection: 'column',
                }, children: [_jsx(ModelSearch, { value: searchQuery, onChange: setSearchQuery, placeholder: "Search models..." }), _jsxs("div", { style: {
                            padding: '6px 12px',
                            borderBottom: '1px solid #eee',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'space-between',
                        }, children: [_jsxs("button", { onClick: () => setShowFilters(!showFilters), style: {
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: 4,
                                    padding: '4px 8px',
                                    fontSize: 12,
                                    border: '1px solid #ddd',
                                    borderRadius: 4,
                                    backgroundColor: showFilters || hasActiveFilters ? '#f0f7ff' : '#fff',
                                    color: hasActiveFilters ? '#2196F3' : '#666',
                                    cursor: 'pointer',
                                }, children: [_jsx("span", { children: "\uD83D\uDD27" }), _jsx("span", { children: "Filters" }), hasActiveFilters && (_jsx("span", { style: {
                                            backgroundColor: '#2196F3',
                                            color: '#fff',
                                            borderRadius: '50%',
                                            width: 16,
                                            height: 16,
                                            fontSize: 10,
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center',
                                        }, children: (selectedProvider ? 1 : 0) + selectedCapabilities.length }))] }), _jsxs("span", { style: { fontSize: 11, color: '#999' }, children: [filteredModels.length, " of ", models.length, " models"] })] }), showFilters && (_jsx("div", { style: { borderBottom: '1px solid #eee' }, children: _jsx(ModelFilter, { providers: availableProviders, capabilities: availableCapabilities, selectedProvider: selectedProvider, selectedCapabilities: selectedCapabilities, onProviderChange: setSelectedProvider, onCapabilitiesChange: setSelectedCapabilities }) })), _jsx("div", { style: {
                            flex: 1,
                            overflowY: 'auto',
                            maxHeight: showFilters ? 240 : 340,
                        }, children: filteredModels.length === 0 ? (_jsx("div", { style: {
                                padding: 24,
                                textAlign: 'center',
                                color: '#999',
                                fontSize: 13,
                            }, children: "No models match your criteria" })) : (filteredModels.map(model => (_jsx(ModelCard, { model: model, isSelected: model.id === selectedModelId, onClick: () => handleSelect(model.id), showCost: showCost, showCapabilities: showCapabilities, maxCapabilitiesToShow: maxCapabilitiesToShow }, model.id)))) })] }))] }));
};
export default ModelSelector;
//# sourceMappingURL=ModelSelector.js.map