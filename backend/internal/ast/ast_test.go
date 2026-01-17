package ast

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSimpleASTParserParse(t *testing.T) {
	parser := NewSimpleASTParser(nil)
	ctx := context.Background()

	// TypeScript代码
	tsCode := `
import { foo } from './foo';
import * as bar from 'bar';

export interface IUser {
	name: string;
	age: number;
}

export class UserService {
	private users: IUser[] = [];

	public getUser(id: string): IUser | null {
		return this.users.find(u => u.id === id) || null;
	}

	async fetchUsers(): Promise<IUser[]> {
		return this.users;
	}
}

export function createUser(name: string): IUser {
	return { name, age: 0 };
}

const MAX_USERS = 100;
`

	result, err := parser.Parse(ctx, tsCode, LangTypeScript)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if result.Language != LangTypeScript {
		t.Errorf("expected typescript, got %s", result.Language)
	}

	// 检查符号
	if len(result.Symbols) == 0 {
		t.Error("expected symbols")
	}

	// 检查是否找到了类
	foundClass := false
	for _, sym := range result.Symbols {
		if sym.Name == "UserService" && sym.Kind == SymbolClass {
			foundClass = true
			break
		}
	}
	if !foundClass {
		t.Error("expected to find UserService class")
	}

	// 检查是否找到了接口
	foundInterface := false
	for _, sym := range result.Symbols {
		if sym.Name == "IUser" && sym.Kind == SymbolInterface {
			foundInterface = true
			break
		}
	}
	if !foundInterface {
		t.Error("expected to find IUser interface")
	}

	// 检查是否找到了函数
	foundFunc := false
	for _, sym := range result.Symbols {
		if sym.Name == "createUser" && sym.Kind == SymbolFunction {
			foundFunc = true
			break
		}
	}
	if !foundFunc {
		t.Error("expected to find createUser function")
	}

	// 检查导入
	if len(result.Imports) == 0 {
		t.Error("expected imports")
	}

	// 检查导出
	if len(result.Exports) == 0 {
		t.Error("expected exports")
	}
}

func TestSimpleASTParserGo(t *testing.T) {
	parser := NewSimpleASTParser(nil)
	ctx := context.Background()

	goCode := `
package main

import (
	"fmt"
	"strings"
)

const MaxSize = 100

type User struct {
	Name string
	Age  int
}

type UserService interface {
	GetUser(id string) *User
	CreateUser(name string) *User
}

func NewUser(name string) *User {
	return &User{Name: name}
}

func (u *User) GetName() string {
	return u.Name
}
`

	result, err := parser.Parse(ctx, goCode, LangGo)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if result.Language != LangGo {
		t.Errorf("expected go, got %s", result.Language)
	}

	// 检查是否找到了struct
	foundStruct := false
	for _, sym := range result.Symbols {
		if sym.Name == "User" && sym.Kind == SymbolClass {
			foundStruct = true
			break
		}
	}
	if !foundStruct {
		t.Error("expected to find User struct")
	}

	// 检查是否找到了interface
	foundInterface := false
	for _, sym := range result.Symbols {
		if sym.Name == "UserService" && sym.Kind == SymbolInterface {
			foundInterface = true
			break
		}
	}
	if !foundInterface {
		t.Error("expected to find UserService interface")
	}

	// 检查是否找到了函数
	foundFunc := false
	for _, sym := range result.Symbols {
		if sym.Name == "NewUser" && sym.Kind == SymbolFunction {
			foundFunc = true
			break
		}
	}
	if !foundFunc {
		t.Error("expected to find NewUser function")
	}
}

func TestSimpleASTParserPython(t *testing.T) {
	parser := NewSimpleASTParser(nil)
	ctx := context.Background()

	pyCode := `
from typing import List, Optional
import json

MAX_SIZE = 100

class User:
    def __init__(self, name: str):
        self.name = name

    def get_name(self) -> str:
        return self.name

def create_user(name: str) -> User:
    return User(name)

async def fetch_users() -> List[User]:
    return []
`

	result, err := parser.Parse(ctx, pyCode, LangPython)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if result.Language != LangPython {
		t.Errorf("expected python, got %s", result.Language)
	}

	// 检查是否找到了class
	foundClass := false
	for _, sym := range result.Symbols {
		if sym.Name == "User" && sym.Kind == SymbolClass {
			foundClass = true
			break
		}
	}
	if !foundClass {
		t.Error("expected to find User class")
	}

	// 检查是否找到了函数
	foundFunc := false
	for _, sym := range result.Symbols {
		if sym.Name == "create_user" && sym.Kind == SymbolFunction {
			foundFunc = true
			break
		}
	}
	if !foundFunc {
		t.Error("expected to find create_user function")
	}
}

func TestSimpleASTParserJava(t *testing.T) {
	parser := NewSimpleASTParser(nil)
	ctx := context.Background()

	javaCode := `
package com.example;

import java.util.List;
import java.util.ArrayList;

public class UserService {
    private static final int MAX_USERS = 100;

    public User getUser(String id) {
        return null;
    }

    public List<User> getAllUsers() {
        return new ArrayList<>();
    }
}

public interface IUserRepository {
    User findById(String id);
}
`

	result, err := parser.Parse(ctx, javaCode, LangJava)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if result.Language != LangJava {
		t.Errorf("expected java, got %s", result.Language)
	}

	// 检查是否找到了class
	foundClass := false
	for _, sym := range result.Symbols {
		if sym.Name == "UserService" && sym.Kind == SymbolClass {
			foundClass = true
			break
		}
	}
	if !foundClass {
		t.Error("expected to find UserService class")
	}

	// 检查是否找到了interface
	foundInterface := false
	for _, sym := range result.Symbols {
		if sym.Name == "IUserRepository" && sym.Kind == SymbolInterface {
			foundInterface = true
			break
		}
	}
	if !foundInterface {
		t.Error("expected to find IUserRepository interface")
	}
}

func TestCountTokens(t *testing.T) {
	parser := NewSimpleASTParser(nil)

	tests := []struct {
		input    string
		minCount int
		maxCount int
	}{
		{"hello world", 2, 5},
		{"func main() { return 0 }", 5, 15},
		{"", 0, 0},
		{"a", 1, 1},
		{"foo.bar(x, y)", 5, 15},
	}

	for _, tt := range tests {
		count := parser.CountTokens(tt.input)
		if count < tt.minCount || count > tt.maxCount {
			t.Errorf("CountTokens(%q) = %d, want between %d and %d",
				tt.input, count, tt.minCount, tt.maxCount)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	parser := NewSimpleASTParser(nil)

	tests := []struct {
		path     string
		expected SupportedLanguage
	}{
		{"foo.ts", LangTypeScript},
		{"foo.tsx", LangTypeScript},
		{"foo.js", LangJavaScript},
		{"foo.jsx", LangJavaScript},
		{"foo.go", LangGo},
		{"foo.py", LangPython},
		{"foo.java", LangJava},
		{"foo.rs", LangRust},
		{"foo.cpp", LangCpp},
		{"foo.c", LangC},
		{"foo.unknown", LangUnknown},
	}

	for _, tt := range tests {
		got := parser.detectLanguage(tt.path)
		if got != tt.expected {
			t.Errorf("detectLanguage(%q) = %s, want %s", tt.path, got, tt.expected)
		}
	}
}

func TestGetSupportedLanguages(t *testing.T) {
	parser := NewSimpleASTParser(nil)

	languages := parser.GetSupportedLanguages()
	if len(languages) == 0 {
		t.Error("expected supported languages")
	}

	// 检查是否包含主要语言
	expected := []SupportedLanguage{LangTypeScript, LangJavaScript, LangGo, LangPython, LangJava}
	for _, exp := range expected {
		found := false
		for _, lang := range languages {
			if lang == exp {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected %s in supported languages", exp)
		}
	}
}

func TestParseFile(t *testing.T) {
	parser := NewSimpleASTParser(nil)
	ctx := context.Background()

	// 创建临时文件
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.go")

	content := `package main

func main() {
	println("Hello")
}

type Config struct {
	Name string
}
`

	if err := os.WriteFile(tmpFile, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	result, err := parser.ParseFile(ctx, tmpFile)
	if err != nil {
		t.Fatalf("parse file: %v", err)
	}

	if result.FilePath != tmpFile {
		t.Errorf("expected file path %s, got %s", tmpFile, result.FilePath)
	}

	if result.Language != LangGo {
		t.Errorf("expected go, got %s", result.Language)
	}

	if len(result.Symbols) == 0 {
		t.Error("expected symbols")
	}
}

func TestContextBuilder(t *testing.T) {
	parser := NewSimpleASTParser(nil)
	builder := NewContextBuilder(parser, 10)
	ctx := context.Background()

	// 创建临时文件
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "test.go")

	var lines []string
	for i := 1; i <= 50; i++ {
		lines = append(lines, "// line "+string(rune('0'+i%10)))
	}
	lines[24] = "func targetFunction() {"
	lines[25] = "    return"
	lines[26] = "}"

	if err := os.WriteFile(tmpFile, []byte(joinLines(lines)), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	// 构建上下文
	window, err := builder.BuildContext(ctx, tmpFile, 25, 10)
	if err != nil {
		t.Fatalf("build context: %v", err)
	}

	if window.FilePath != tmpFile {
		t.Errorf("expected file path %s, got %s", tmpFile, window.FilePath)
	}

	if window.StartLine < 20 || window.StartLine > 25 {
		t.Errorf("unexpected start line: %d", window.StartLine)
	}

	if window.EndLine < 25 || window.EndLine > 35 {
		t.Errorf("unexpected end line: %d", window.EndLine)
	}

	if window.Content == "" {
		t.Error("expected content")
	}

	if window.TokenCount == 0 {
		t.Error("expected token count > 0")
	}
}

func TestTruncateToTokenLimit(t *testing.T) {
	parser := NewSimpleASTParser(nil)
	builder := NewContextBuilder(parser, 10)

	content := "line1\nline2\nline3\nline4\nline5"

	// 非常小的限制应该截断
	truncated := builder.TruncateToTokenLimit(content, 3)
	if len(truncated) >= len(content) {
		t.Error("expected truncation with small limit")
	}

	// 大限制应该保持不变
	notTruncated := builder.TruncateToTokenLimit(content, 1000)
	if notTruncated != content {
		t.Error("expected no truncation with large limit")
	}

	// 零限制应该返回原始内容
	noLimit := builder.TruncateToTokenLimit(content, 0)
	if noLimit != content {
		t.Error("expected no truncation with zero limit")
	}
}

func TestEstimateTokensGPT(t *testing.T) {
	tests := []struct {
		input    string
		minCount int
		maxCount int
	}{
		{"hello", 1, 3},
		{"hello world", 2, 5},
		{"", 0, 1},
		{"你好世界", 1, 4},
	}

	for _, tt := range tests {
		count := EstimateTokensGPT(tt.input)
		if count < tt.minCount || count > tt.maxCount {
			t.Errorf("EstimateTokensGPT(%q) = %d, want between %d and %d",
				tt.input, count, tt.minCount, tt.maxCount)
		}
	}
}

func TestExtractSymbols(t *testing.T) {
	parser := NewSimpleASTParser(nil)
	ctx := context.Background()

	code := `
export class MyClass {
	myMethod() {}
}

export function myFunction() {}

export const MY_CONSTANT = 42;
`

	symbols, err := parser.ExtractSymbols(ctx, code, LangTypeScript)
	if err != nil {
		t.Fatalf("extract symbols: %v", err)
	}

	if len(symbols) == 0 {
		t.Error("expected symbols")
	}

	// 验证符号类型
	kindCounts := make(map[SymbolKind]int)
	for _, sym := range symbols {
		kindCounts[sym.Kind]++
	}

	if kindCounts[SymbolClass] == 0 {
		t.Error("expected at least one class")
	}
	if kindCounts[SymbolFunction] == 0 {
		t.Error("expected at least one function")
	}
}

func TestContextCancellation(t *testing.T) {
	parser := NewSimpleASTParser(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	_, err := parser.Parse(ctx, "some code", LangGo)
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
