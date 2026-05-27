import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
/**
 * CustomSelect - 自定义下拉选择器
 * 统一风格的下拉组件，替代原生 select
 * 支持分组选项、多选模式、异步加载
 */
import { useState, useRef, useEffect, useCallback } from 'react';
import { colors, borderRadius, fontSize, fontWeight, shadows, spacing, transitions } from './tokens';
const sizeConfig = {
    sm: { padding: '6px 10px', fontSize: 12, iconSize: 14 },
    md: { padding: '8px 12px', fontSize: 13, iconSize: 16 },
    lg: { padding: '10px 14px', fontSize: 14, iconSize: 18 },
};
export const CustomSelect = (props) => {
    const { options = [], groupedOptions, value, onChange, placeholder = 'Select...', disabled = false, loading = false, searchable = false, clearable = false, multiple = false, size = 'md', variant = 'default', error = false, helperText, label, className, style, dropdownStyle, renderOption, renderValue, onLoadMore, hasMore = false, loadingMore = false, } = props;
    const maxSelections = multiple ? props.maxSelections : undefined;
    const [isOpen, setIsOpen] = useState(false);
    const [searchQuery, setSearchQuery] = useState('');
    const [highlightedIndex, setHighlightedIndex] = useState(-1);
    const containerRef = useRef(null);
    const inputRef = useRef(null);
    const listRef = useRef(null);
    // Flatten grouped options if provided
    const allOptions = groupedOptions
        ? groupedOptions.flatMap(group => group.options.map(opt => ({ ...opt, group: group.label })))
        : options;
    // Get selected values as array
    const selectedValues = multiple
        ? (Array.isArray(value) ? value : value ? [value] : [])
        : (value ? [value] : []);
    const selectedOptions = allOptions.filter(opt => selectedValues.includes(opt.value));
    const sizeStyles = sizeConfig[size];
    // Filter options
    const filteredOptions = searchQuery
        ? allOptions.filter(opt => opt.label.toLowerCase().includes(searchQuery.toLowerCase()) ||
            opt.description?.toLowerCase().includes(searchQuery.toLowerCase()))
        : allOptions;
    // Group filtered options
    const getGroupedFilteredOptions = () => {
        if (!groupedOptions)
            return null;
        return groupedOptions
            .map(group => ({
            ...group,
            options: group.options.filter(opt => !searchQuery ||
                opt.label.toLowerCase().includes(searchQuery.toLowerCase()) ||
                opt.description?.toLowerCase().includes(searchQuery.toLowerCase())),
        }))
            .filter(group => group.options.length > 0);
    };
    // Click outside to close
    useEffect(() => {
        const handleClickOutside = (event) => {
            if (containerRef.current && !containerRef.current.contains(event.target)) {
                setIsOpen(false);
                setSearchQuery('');
            }
        };
        document.addEventListener('mousedown', handleClickOutside);
        return () => document.removeEventListener('mousedown', handleClickOutside);
    }, []);
    // Scroll to load more
    useEffect(() => {
        if (!isOpen || !onLoadMore || !hasMore || loadingMore)
            return;
        const handleScroll = () => {
            if (!listRef.current)
                return;
            const { scrollTop, scrollHeight, clientHeight } = listRef.current;
            if (scrollHeight - scrollTop - clientHeight < 50) {
                onLoadMore();
            }
        };
        const list = listRef.current;
        list?.addEventListener('scroll', handleScroll);
        return () => list?.removeEventListener('scroll', handleScroll);
    }, [isOpen, onLoadMore, hasMore, loadingMore]);
    // Keyboard navigation
    const handleKeyDown = useCallback((event) => {
        if (disabled)
            return;
        switch (event.key) {
            case 'Enter':
                event.preventDefault();
                if (isOpen && highlightedIndex >= 0 && filteredOptions[highlightedIndex]) {
                    const opt = filteredOptions[highlightedIndex];
                    if (!opt.disabled) {
                        handleSelect(opt);
                    }
                }
                else {
                    setIsOpen(!isOpen);
                }
                break;
            case 'Escape':
                setIsOpen(false);
                setSearchQuery('');
                break;
            case 'ArrowDown':
                event.preventDefault();
                if (!isOpen) {
                    setIsOpen(true);
                }
                else {
                    setHighlightedIndex(prev => prev < filteredOptions.length - 1 ? prev + 1 : 0);
                }
                break;
            case 'ArrowUp':
                event.preventDefault();
                if (isOpen) {
                    setHighlightedIndex(prev => prev > 0 ? prev - 1 : filteredOptions.length - 1);
                }
                break;
            case 'Tab':
                if (isOpen) {
                    setIsOpen(false);
                    setSearchQuery('');
                }
                break;
        }
    }, [disabled, isOpen, highlightedIndex, filteredOptions]);
    const handleSelect = (opt) => {
        if (opt.disabled)
            return;
        if (multiple) {
            const currentValues = selectedValues;
            const isSelected = currentValues.includes(opt.value);
            let newValues;
            if (isSelected) {
                newValues = currentValues.filter(v => v !== opt.value);
            }
            else {
                if (maxSelections && currentValues.length >= maxSelections) {
                    return;
                }
                newValues = [...currentValues, opt.value];
            }
            onChange?.(newValues);
        }
        else {
            onChange?.(opt.value);
            setIsOpen(false);
            setSearchQuery('');
        }
    };
    const handleClear = (e) => {
        e.stopPropagation();
        if (multiple) {
            onChange?.([]);
        }
        else {
            onChange?.('');
        }
    };
    const handleRemoveTag = (e, valueToRemove) => {
        e.stopPropagation();
        if (multiple) {
            onChange?.(selectedValues.filter(v => v !== valueToRemove));
        }
    };
    const getVariantStyles = () => {
        const base = {
            display: 'flex',
            alignItems: 'center',
            gap: spacing[2],
            padding: sizeStyles.padding,
            fontSize: sizeStyles.fontSize,
            fontWeight: fontWeight.medium,
            borderRadius: borderRadius.xl,
            cursor: disabled ? 'not-allowed' : 'pointer',
            opacity: disabled ? 0.6 : 1,
            transition: transitions.fast,
            minWidth: 180,
            flexWrap: multiple ? 'wrap' : 'nowrap',
        };
        switch (variant) {
            case 'outlined':
                return {
                    ...base,
                    backgroundColor: 'transparent',
                    border: `1px solid ${error ? colors.error.main : isOpen ? colors.primary[400] : colors.slate[300]}`,
                    color: colors.slate[700],
                };
            case 'filled':
                return {
                    ...base,
                    backgroundColor: colors.slate[100],
                    border: `1px solid transparent`,
                    color: colors.slate[700],
                };
            default:
                return {
                    ...base,
                    backgroundColor: '#fff',
                    border: `1px solid ${error ? colors.error.main : isOpen ? colors.primary[400] : colors.slate[200]}`,
                    boxShadow: isOpen ? `0 0 0 3px ${colors.primary[100]}` : shadows.sm,
                    color: colors.slate[700],
                };
        }
    };
    const renderSelectedValue = () => {
        if (loading) {
            return _jsx("span", { style: { color: colors.slate[400] }, children: "Loading..." });
        }
        if (selectedOptions.length === 0) {
            return _jsx("span", { style: { color: colors.slate[400], flex: 1 }, children: placeholder });
        }
        if (renderValue) {
            if (multiple) {
                return renderValue(selectedOptions);
            }
            return renderValue(selectedOptions[0]);
        }
        if (multiple) {
            return (_jsx("div", { style: { display: 'flex', flexWrap: 'wrap', gap: spacing[1], flex: 1 }, children: selectedOptions.map(opt => (_jsxs("span", { style: {
                        display: 'inline-flex',
                        alignItems: 'center',
                        gap: spacing[1],
                        padding: `${spacing[0.5]}px ${spacing[2]}px`,
                        backgroundColor: colors.primary[100],
                        color: colors.primary[700],
                        borderRadius: borderRadius.full,
                        fontSize: fontSize.xs,
                    }, children: [opt.label, _jsx("button", { onClick: (e) => handleRemoveTag(e, opt.value), style: {
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                width: 14,
                                height: 14,
                                borderRadius: '50%',
                                backgroundColor: colors.primary[200],
                                border: 'none',
                                cursor: 'pointer',
                                fontSize: 10,
                                color: colors.primary[600],
                                padding: 0,
                            }, children: "\u00D7" })] }, opt.value))) }));
        }
        const opt = selectedOptions[0];
        return (_jsxs(_Fragment, { children: [opt.icon && (_jsx("span", { style: { display: 'flex', alignItems: 'center' }, children: opt.icon })), _jsx("span", { style: { flex: 1 }, children: opt.label }), opt.color && (_jsx("span", { style: {
                        width: 8,
                        height: 8,
                        borderRadius: '50%',
                        backgroundColor: opt.color,
                    } }))] }));
    };
    const renderOptionItem = (opt, index, isInGroup = false) => {
        const isSelected = selectedValues.includes(opt.value);
        const isHighlighted = index === highlightedIndex;
        if (renderOption) {
            return (_jsx("div", { onClick: () => handleSelect(opt), onMouseEnter: () => setHighlightedIndex(index), children: renderOption(opt, isSelected) }, opt.value));
        }
        return (_jsxs("div", { onClick: () => handleSelect(opt), onMouseEnter: () => setHighlightedIndex(index), style: {
                display: 'flex',
                alignItems: 'center',
                gap: spacing[2.5],
                padding: `${spacing[2.5]}px ${spacing[3]}px`,
                paddingLeft: isInGroup ? spacing[6] : spacing[3],
                cursor: opt.disabled ? 'not-allowed' : 'pointer',
                opacity: opt.disabled ? 0.5 : 1,
                backgroundColor: isSelected
                    ? colors.primary[50]
                    : isHighlighted
                        ? colors.slate[50]
                        : 'transparent',
                borderLeft: isSelected ? `3px solid ${colors.primary[500]}` : '3px solid transparent',
                transition: transitions.fast,
            }, role: "option", "aria-selected": isSelected, children: [multiple && (_jsx("span", { style: {
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: 16,
                        height: 16,
                        borderRadius: borderRadius.sm,
                        border: `2px solid ${isSelected ? colors.primary[500] : colors.slate[300]}`,
                        backgroundColor: isSelected ? colors.primary[500] : 'transparent',
                        color: '#fff',
                        fontSize: 10,
                    }, children: isSelected && '✓' })), opt.icon && (_jsx("span", { style: {
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'center',
                        width: 24,
                        height: 24,
                        borderRadius: borderRadius.md,
                        backgroundColor: opt.color ? `${opt.color}15` : colors.slate[100],
                        color: opt.color || colors.slate[600],
                    }, children: opt.icon })), _jsxs("div", { style: { flex: 1, minWidth: 0 }, children: [_jsx("div", { style: {
                                fontSize: fontSize.sm,
                                fontWeight: isSelected ? fontWeight.semibold : fontWeight.medium,
                                color: isSelected ? colors.primary[700] : colors.slate[700],
                            }, children: opt.label }), opt.description && (_jsx("div", { style: {
                                fontSize: fontSize.xs,
                                color: colors.slate[500],
                                marginTop: 2,
                                overflow: 'hidden',
                                textOverflow: 'ellipsis',
                                whiteSpace: 'nowrap',
                            }, children: opt.description }))] }), !multiple && isSelected && (_jsx("span", { style: { color: colors.primary[500], fontSize: 14 }, children: "\u2713" }))] }, opt.value));
    };
    const renderOptions = () => {
        const groupedFiltered = getGroupedFilteredOptions();
        if (groupedFiltered) {
            let globalIndex = 0;
            return groupedFiltered.map(group => (_jsxs("div", { children: [_jsx("div", { style: {
                            padding: `${spacing[2]}px ${spacing[3]}px`,
                            fontSize: fontSize.xs,
                            fontWeight: fontWeight.bold,
                            color: colors.slate[500],
                            textTransform: 'uppercase',
                            letterSpacing: '0.05em',
                            backgroundColor: colors.slate[50],
                            borderBottom: `1px solid ${colors.slate[100]}`,
                        }, children: group.label }), group.options.map(opt => {
                        const item = renderOptionItem(opt, globalIndex, true);
                        globalIndex++;
                        return item;
                    })] }, group.label)));
        }
        return filteredOptions.map((opt, index) => renderOptionItem(opt, index));
    };
    return (_jsxs("div", { ref: containerRef, className: className, style: { position: 'relative', display: 'inline-block', ...style }, children: [label && (_jsx("label", { style: {
                    display: 'block',
                    marginBottom: spacing[1.5],
                    fontSize: fontSize.xs,
                    fontWeight: fontWeight.bold,
                    color: colors.slate[700],
                    textTransform: 'uppercase',
                    letterSpacing: '0.05em',
                }, children: label })), _jsxs("div", { onClick: () => !disabled && setIsOpen(!isOpen), onKeyDown: handleKeyDown, tabIndex: disabled ? -1 : 0, style: getVariantStyles(), role: "combobox", "aria-expanded": isOpen, "aria-haspopup": "listbox", "aria-multiselectable": multiple, children: [renderSelectedValue(), clearable && selectedOptions.length > 0 && !disabled && (_jsx("button", { onClick: handleClear, style: {
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            width: 16,
                            height: 16,
                            borderRadius: '50%',
                            backgroundColor: colors.slate[200],
                            border: 'none',
                            cursor: 'pointer',
                            fontSize: 10,
                            color: colors.slate[500],
                            flexShrink: 0,
                        }, children: "\u00D7" })), _jsx("span", { style: {
                            marginLeft: 'auto',
                            transform: isOpen ? 'rotate(180deg)' : 'rotate(0deg)',
                            transition: transitions.fast,
                            fontSize: 10,
                            color: colors.slate[400],
                            flexShrink: 0,
                        }, children: "\u25BC" })] }), helperText && (_jsx("span", { style: {
                    display: 'block',
                    marginTop: spacing[1],
                    fontSize: fontSize.xs,
                    color: error ? colors.error.main : colors.slate[500],
                }, children: helperText })), isOpen && (_jsxs("div", { style: {
                    position: 'absolute',
                    top: '100%',
                    left: 0,
                    right: 0,
                    marginTop: spacing[1],
                    backgroundColor: '#fff',
                    border: `1px solid ${colors.slate[200]}`,
                    borderRadius: borderRadius.xl,
                    boxShadow: shadows.xl,
                    zIndex: 1000,
                    overflow: 'hidden',
                    animation: 'slideDown 0.15s ease',
                    ...dropdownStyle,
                }, role: "listbox", children: [searchable && (_jsx("div", { style: { padding: spacing[2], borderBottom: `1px solid ${colors.slate[100]}` }, children: _jsx("input", { ref: inputRef, type: "text", value: searchQuery, onChange: (e) => setSearchQuery(e.target.value), placeholder: "Search...", autoFocus: true, style: {
                                width: '100%',
                                padding: `${spacing[1.5]}px ${spacing[2.5]}px`,
                                fontSize: fontSize.sm,
                                border: `1px solid ${colors.slate[200]}`,
                                borderRadius: borderRadius.lg,
                                backgroundColor: colors.slate[50],
                                outline: 'none',
                            } }) })), _jsx("div", { ref: listRef, style: { maxHeight: 280, overflowY: 'auto' }, children: filteredOptions.length === 0 && !loadingMore ? (_jsx("div", { style: {
                                padding: spacing[4],
                                textAlign: 'center',
                                color: colors.slate[400],
                                fontSize: fontSize.sm,
                            }, children: "No options found" })) : (_jsxs(_Fragment, { children: [renderOptions(), loadingMore && (_jsx("div", { style: {
                                        padding: spacing[3],
                                        textAlign: 'center',
                                        color: colors.slate[400],
                                        fontSize: fontSize.sm,
                                    }, children: "Loading more..." })), hasMore && !loadingMore && (_jsx("div", { style: {
                                        padding: spacing[2],
                                        textAlign: 'center',
                                    }, children: _jsx("button", { onClick: (e) => {
                                            e.stopPropagation();
                                            onLoadMore?.();
                                        }, style: {
                                            padding: `${spacing[1]}px ${spacing[3]}px`,
                                            fontSize: fontSize.xs,
                                            color: colors.primary[600],
                                            backgroundColor: 'transparent',
                                            border: `1px solid ${colors.primary[200]}`,
                                            borderRadius: borderRadius.md,
                                            cursor: 'pointer',
                                        }, children: "Load more" }) }))] })) }), multiple && selectedValues.length > 0 && (_jsxs("div", { style: {
                            padding: `${spacing[2]}px ${spacing[3]}px`,
                            borderTop: `1px solid ${colors.slate[100]}`,
                            backgroundColor: colors.slate[50],
                            fontSize: fontSize.xs,
                            color: colors.slate[500],
                            display: 'flex',
                            justifyContent: 'space-between',
                            alignItems: 'center',
                        }, children: [_jsxs("span", { children: [selectedValues.length, " selected"] }), maxSelections && (_jsxs("span", { children: ["Max: ", maxSelections] }))] }))] })), _jsx("style", { children: `
        @keyframes slideDown {
          from {
            opacity: 0;
            transform: translateY(-8px);
          }
          to {
            opacity: 1;
            transform: translateY(0);
          }
        }
      ` })] }));
};
export default CustomSelect;
//# sourceMappingURL=CustomSelect.js.map