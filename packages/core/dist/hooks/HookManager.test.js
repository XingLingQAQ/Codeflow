import { HookManager, HookEvent } from '../src/hooks';
describe('HookManager', () => {
    let hookManager;
    beforeEach(() => {
        hookManager = new HookManager();
    });
    afterEach(() => {
        hookManager.clear();
    });
    describe('Hook 触发顺序', () => {
        it('应按注册顺序执行 hook_before_send 处理器', async () => {
            const executionOrder = [];
            hookManager.register(HookEvent.BEFORE_SEND, async (payload) => {
                executionOrder.push(1);
                return payload;
            });
            hookManager.register(HookEvent.BEFORE_SEND, async (payload) => {
                executionOrder.push(2);
                return payload;
            });
            hookManager.register(HookEvent.BEFORE_SEND, async (payload) => {
                executionOrder.push(3);
                return payload;
            });
            const payload = {
                messages: [{ role: 'user', content: 'test' }],
            };
            await hookManager.hook_before_send(payload);
            expect(executionOrder).toEqual([1, 2, 3]);
        });
        it('应按注册顺序执行 hook_post_response 处理器', async () => {
            const executionOrder = [];
            hookManager.register(HookEvent.POST_RESPONSE, async () => {
                executionOrder.push(1);
            });
            hookManager.register(HookEvent.POST_RESPONSE, async () => {
                executionOrder.push(2);
            });
            const response = {
                content: 'test response',
                model: 'claude-3',
            };
            await hookManager.hook_post_response(response);
            expect(executionOrder).toEqual([1, 2]);
        });
        it('应同步执行 hook_on_stream 处理器', () => {
            const executionOrder = [];
            hookManager.register(HookEvent.ON_STREAM, (chunk) => {
                executionOrder.push(1);
            });
            hookManager.register(HookEvent.ON_STREAM, (chunk) => {
                executionOrder.push(2);
            });
            const chunk = {
                delta: 'test',
                index: 0,
                done: false,
            };
            hookManager.hook_on_stream(chunk);
            expect(executionOrder).toEqual([1, 2]);
        });
    });
    describe('并发场景', () => {
        it('应正确处理并发的 hook_before_send 调用', async () => {
            let counter = 0;
            hookManager.register(HookEvent.BEFORE_SEND, async (payload) => {
                await new Promise((resolve) => setTimeout(resolve, 10));
                counter++;
                return payload;
            });
            const payload = {
                messages: [{ role: 'user', content: 'test' }],
            };
            await Promise.all([
                hookManager.hook_before_send(payload),
                hookManager.hook_before_send(payload),
                hookManager.hook_before_send(payload),
            ]);
            expect(counter).toBe(3);
        });
        it('应正确处理并发的 hook_on_stream 调用', () => {
            const chunks = [];
            hookManager.register(HookEvent.ON_STREAM, (chunk) => {
                chunks.push(chunk.delta);
            });
            const chunk1 = { delta: 'a', index: 0, done: false };
            const chunk2 = { delta: 'b', index: 1, done: false };
            const chunk3 = { delta: 'c', index: 2, done: true };
            hookManager.hook_on_stream(chunk1);
            hookManager.hook_on_stream(chunk2);
            hookManager.hook_on_stream(chunk3);
            expect(chunks).toEqual(['a', 'b', 'c']);
        });
        it('应在高并发场景下保持 Hook 执行稳定', async () => {
            const results = [];
            hookManager.register(HookEvent.POST_RESPONSE, async () => {
                await new Promise((resolve) => setTimeout(resolve, Math.random() * 10));
                results.push(1);
            });
            const response = {
                content: 'test',
                model: 'claude-3',
            };
            const promises = Array.from({ length: 100 }, () => hookManager.hook_post_response(response));
            await Promise.all(promises);
            expect(results.length).toBe(100);
        });
    });
    describe('Hook 注册与注销', () => {
        it('应正确注册和注销处理器', async () => {
            let counter = 0;
            const handler = async () => {
                counter++;
            };
            hookManager.register(HookEvent.POST_RESPONSE, handler);
            const response = {
                content: 'test',
                model: 'claude-3',
            };
            await hookManager.hook_post_response(response);
            expect(counter).toBe(1);
            hookManager.unregister(HookEvent.POST_RESPONSE, handler);
            await hookManager.hook_post_response(response);
            expect(counter).toBe(1); // 不应再增加
        });
        it('应正确清理所有处理器', async () => {
            let counter = 0;
            hookManager.register(HookEvent.POST_RESPONSE, async () => {
                counter++;
            });
            hookManager.register(HookEvent.BEFORE_SEND, async (payload) => {
                counter++;
                return payload;
            });
            hookManager.clear();
            const response = {
                content: 'test',
                model: 'claude-3',
            };
            await hookManager.hook_post_response(response);
            const payload = {
                messages: [{ role: 'user', content: 'test' }],
            };
            await hookManager.hook_before_send(payload);
            expect(counter).toBe(0);
        });
    });
    describe('EventEmitter 集成', () => {
        it('应触发 EventEmitter 事件', (done) => {
            const payload = {
                messages: [{ role: 'user', content: 'test' }],
            };
            hookManager.on(HookEvent.BEFORE_SEND, (data, result) => {
                expect(data).toEqual(payload);
                expect(result).toEqual(payload);
                done();
            });
            hookManager.hook_before_send(payload);
        });
    });
});
//# sourceMappingURL=HookManager.test.js.map