---
title: iOS .so 调研
layout: page
---

# iOS `.so`：能不能像 Android 那样在本机加载执行？

调研 note，回答「仿照 Android/golem 方案支持 iOS 的 `.so`」这件事现在能做到哪一步。
结论基于一次在本机（macOS/amd64，unicorn 引擎）真跑的实验，不是读文档推断。

**结论先给：加载与调用已经通了，卡点不在加载器，在运行时符号。**

---

## 1. 实验

用 golem 的公开 API（不需要 `internal/` 包），把一个真正的 Mach-O 64 位 arm64 dylib 喂进去：

```go
e, err := emulator.New(emulator.Config{
    SOPath: "<golem>/examples/native/hello_darwin_arm64.dylib",
    Engine: "unicorn",
    Pid:    4242,
})
defer e.Close()
v, err := e.CallSymbol("add", emulator.Words(3, 4)...)
```

实测输出（macOS 14 / darwin amd64 宿主，arm64 guest）：

```
booted: engine=unicorn
CallSymbol("add")           -> 0x7     err=<nil>     # 3+4
CallSymbol("guest_getpid")  -> 0x1092  err=<nil>     # 4242，Darwin 系统调用通道是通的
CallSymbol("_add")          -> 0x0     err=symbol "_add" not found
CallSymbol("malloc")        -> 0x0     err=symbol "malloc" not found
CallSymbol("memcpy")        -> 0x0     err=symbol "memcpy" not found
```

三件事被同时证明了：

1. **容器格式没问题。** golem v0.2.1 的 loader 已经解析 Mach-O（含 `LC_DYLD_INFO` 经典 rebase/bind 与 `LC_DYLD_CHAINED_FIXUPS` 两种），启动时按魔数自动选择 Darwin 人格——**不需要传任何 platform 配置**。
2. **Darwin 人格是活的，但很薄。** `getpid` 走的是 x16 + `svc #0x80` 的 Darwin 系统调用通道，返回值正确；golem 自己对这个包的注释写的是「syscall table 只绑了 e2e 用到的那一族（pid/uid），其余 `Dispatch` 一律 ENOSYS」。
3. **符号名不要加下划线。** Mach-O 符号表里是 `_add`，golem 的 `CallSymbol` 收的是去掉前缀的 `add`。这是一个会让人以为「加载失败」的坑。

## 2. 真正的缺口：`libSystem` 那一层

Android 能跑，是因为 golem 自带 bionic 的资产（`assets/android/sdk23/...`，`libc/libm/libdl` 都是可映射的真实 `.so`）。iOS 没有对应物，也不可能有：Apple 不分发可再链接的 libSystem/dyld 共享缓存。

上面的实验里 `malloc` / `memcpy` 直接 `symbol not found`——它们既不在 guest 镜像里，也没有可映射的宿主库。于是：

- **纯计算**（签名、加解密、序列化、算法校验）：**今天就能跑**。很多逆向场景里的 `.so` 正是这一类，而且这正是 runvm 的主场景（`android_call` + `android_hook` 那套）。
- **碰 libc / Objective-C / Swift runtime 的**：需要在宿主侧按符号名提供实现。Android 路线在 golem 里是 `ReplaceFns`（`android.Config`，runvm 已经在用）；Darwin 路线的同一个能力在 `internal/platform/darwin.Config.ReplaceFns`，参数类型是 `internal/interpose.HostFunc`——**外部模块 import 不到**。这是当前唯一的上游阻塞点。

## 3. 落地顺序（建议）

按「每一步都能单独验证」拆，不要一次铺开：

1. **runvm：按格式分流，别硬套 Android 人格。**
   `android.Open` 现在无条件塞 `Config.Android`（bionic 资产检查 + JNI handler）。Mach-O 应该跳过资产检查、跳过 JNI，让 golem 自己按魔数选 Darwin。
   验收：`ios_open {dylib}` → `ios_call {symbol:"add", args:[3,4]}` → `7`。
2. **工具命名：加 `ios_*`，不叫 `android_*`。** 语义、ABI、能做的事都不一样，混在一个工具里模型会误用。也别急着把两者合并成 `native_*`——等 iOS 路径稳定再谈抽象。
3. **上游（golem）：把 Darwin 的 host 函数注册开到公开 API。**
   需要的形状和 `emulator.WithPlatformConfig` 一样，只是 `interpose.HostFunc` 要能被外部实现（或者给一个 `emulator.WithHostFuncs(map[string]func(*Hook) uint64)` 的门面）。没有这一步，第 4 步做不了。
4. **`ios_hook`：给 libSystem / ObjC 的常用符号做可注册桩。**
   先做一小撮：`malloc/free/memcpy/memset/strlen`、`objc_msgSend`（返回桩值）、`arc4random`、时间函数。runvm 在 Android 侧已经有 `android_hook` 的形状可以直接照搬。
5. **文档与真实度：如实说边界。** 「模拟执行，不是沙箱；纯函数可跑，依赖系统库的需要桩」。不要写「支持 iOS」。

## 4. 已知风险

- **iOS 专有 ABI**：真机 `.dylib` 可能用到 arm64e（指针认证）。golem 有 arm64e 的 fixture 与测试，但这次实验只验了 arm64。
- **Bitcode / Swift**：老 iOS 库可能带 bitcode（Xcode 14 后已废弃）；Swift 运行时符号极多，别指望默认能跑。
- **符号可见性**：很多 iOS 库只导出少数符号，或者被 strip 过；拿不到导出表时要给出明确错误，而不是「会话已打开」。
- **与 Android 的资产隔离**：Darwin 引导不能去碰 bionic 资产树（golem 的 Darwin 测试明确不传 `AssetRoot`），runvm 的分支里要照做，否则会在没装 bionic 的机器上报错。

## 5. 复现这次实验

```bash
# 引擎：libunicorn（macOS 上 pip 的 wheel 里就有一个）
python3 -m pip install --break-system-packages unicorn
export GOLEM_UNICORN=$(python3 -c 'import os,unicorn;print(os.path.join(os.path.dirname(unicorn.__file__),"lib","libunicorn.2.dylib"))')

# fixture：golem 仓库自带的 Mach-O arm64 dylib
DYLIB="$(go list -m -f '{{.Dir}}' github.com/isesword/golem)/examples/native/hello_darwin_arm64.dylib"
```

在 runvm 里按第 1 步分流之后，这段就是 `ios_open` 的实现主干。
