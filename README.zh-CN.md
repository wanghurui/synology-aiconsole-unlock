# aiconsole-unlock

给 Synology AI Console 打一个补丁，**不用绑定群晖账号**就能正常使用。

原版在你添加 AI 服务商之前，会要求先跳转 `activation.synology.com` 完成激活，
并且把激活结果存进数据库。这个校验在 `libsynoai.so` 里只有一条指令，本工具把它
改成常量，于是控制台始终认为自己已激活，「登录您的 Synology 账号」的提示不再出现。

适用于 DSM 7.x。已在 DS918+ / `AIConsole 1.2.0-2086` 上实测。

---

## 原理

`SYNO.AI.Activation&method=get` 由 `libsynoai.so` 的
`synoai::activation::GetActivation()`（`0x63160`）实现。它从 `ai_activation`
表读一行记录，然后写回结果对象：

```asm
0x631f4   40 88 6b 20    mov byte ptr [rbx+0x20], bpl   ; bpl = 记录是否存在
0x631f8   4c 89 73 28    mov qword ptr [rbx+0x28], r14  ; activation_time
```

`[rbx+0x20]` 就是 webapi 返回 JSON 里的 `activated` 字段。没激活时表是空的，
`bpl` 为 `0`，前端就拦住所有添加服务商的操作。把这条 store 换成常量：

```asm
0x631f4   c6 43 20 01    mov byte ptr [rbx+0x20], 1
```

`activated` 就恒为 `true`。

由这个改动可以推出两件事，都已在真机验证：

* **不查数据库。** `ai_activation` 保持 `NULL, NULL` 也照样报 `activated:true`。
* **不用重启。** `SYNO.AI` 是按请求 fork 的 CGI，下一次调用就加载到打过补丁的库。

模型转发进程（`daemon/api-proxy`，135 MB 的 Go 二进制）里没有任何
`synoai::activation` 或 `libsynoai` 引用，所以转发层不校验激活。webapi 是唯一的门。

### 前端怎么用这个标志

`ui/dist/bundle.iife.js` 里 `ProviderPanel` 组件：

```js
l.activated ? t.openModalWindow(jm, G)   // 直接开「添加服务商」对话框
            : await F(G)                 // F() 才是「登录群晖账号」流程
```

`l` 就是 `SYNO.AI.Activation get` 的返回值。整个 bundle 里没有别的地方读激活状态，
`activation_time` 甚至一次都没出现，所以它保持 `0` 没有任何影响。

---

## 安装

按你的 NAS 架构选一个文件，传上去，跑一次。

```sh
# 在 NAS 上，root 身份
sudo ./aiconsole-unlock            # 空跑，只显示会改什么
sudo ./aiconsole-unlock -apply     # 真正写入
```

预期输出：

```
library : /volume1/@appstore/AIConsole/lib/libsynoai.so
size    : 1452585 bytes
md5     : 09214dbb6645b69051fa8d192600f880
state   : unpatched

patch   : offset 0x631f4
          before 40886b20
          after  c6432001
backup  : created /volume1/@appstore/AIConsole/lib/libsynoai.so.orig
written : /volume1/@appstore/AIConsole/lib/libsynoai.so
owner   : AIConsole:AIConsole

result  : unlocked
```

验证：

```sh
curl -s 'http://127.0.0.1:5000/webapi/entry.cgi?api=SYNO.AI.Activation&version=1&method=get'
# {"data":{"activated":true,"activation_time":0,"serial_number":"..."},"success":true}
```

刷新 AI Console 页面，**Add API Provider** 会直接打开服务商对话框。

### 架构对照

| 文件 | 适用 |
| --- | --- |
| `aiconsole-unlock-linux-amd64` | x86-64 机型（DS918+、DS1621+、DS923+ 等绝大多数 plus 型号） |
| `aiconsole-unlock-linux-arm64` | ARMv8（DS223、DS423、RT6600ax、较新的 value 型号） |
| `aiconsole-unlock-linux-armv7` | ARMv7（DS218、DS418、DS118、较旧的 value 型号） |

用 `uname -m` 确认：`x86_64`、`aarch64`、`armv7l`。

### 自己编译

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o aiconsole-unlock
```

只用标准库，没有第三方依赖。

---

## 参数

| 参数 | 含义 |
| --- | --- |
| *（不带参数）* | 空跑，报告状态和将要做的改动，不写盘 |
| `-apply` | 写入补丁 |
| `-status` | 只报告状态 |
| `-revert` | 从 `<path>.orig` 还原 |
| `-f PATH` | 指定非默认路径的库文件 |
| `-owner U:G` | 打完后设置的属主（默认 `AIConsole:AIConsole`） |
| `-no-chown` | 不改属主 |

工具是幂等的：对已打补丁的库再跑 `-apply` 只打印 `already patched` 并以 `0` 退出，
不写盘。签名匹配 0 次（不支持的版本）或多次（有歧义）时它拒绝动手，所以不会把
意外的文件改坏。

---

## 升级之后

群晖的套件升级会覆盖 `libsynoai.so`，补丁随之消失。重跑一次即可：

```sh
sudo aiconsole-unlock -apply
```

如果函数位置移了但代码没变，扫描器照样能找到。如果群晖改写了这段逻辑，工具会报
`signature not found` 而不是乱改——如何重新定位见 [docs/internals.md](docs/internals.md)。

现成的包装脚本在 [scripts/post-upgrade.sh](scripts/post-upgrade.sh)，可以挂到
「控制面板 > 任务计划 > 触发的任务 > 开机」上，实现每次开机自动补。

## 卸载

```sh
sudo aiconsole-unlock -revert     # 还原原始库
```

或者直接在套件中心重装 AIConsole。

---

## 已验证的过程

在 DS918+（DSM 7.2.2，AIConsole 1.2.0-2086）上用真实 SID 做了往返测试：

| 步骤 | `SYNO.AI.Activation get` |
| --- | --- |
| 原版 | `{"activated":false,...}` |
| `-apply` | `{"activated":true,...}` |
| `-revert` | `{"activated":false,...}` |
| 再次 `-apply` | `{"activated":true,...}` |

之后通过 UI 添加一个 `openai_compat` 服务商可以完整走通。

## 说明

* 库文件大小不变，只差 4 个字节（`40 88 6b 20` → `c6 43 20 01`）。
* `.orig` 备份只创建一次，之后不再覆盖，所以重复 `-apply` 不会毁掉你唯一的干净副本。
* 实测版本的补丁后 md5：`12d3ef765abf0b71c727ed1cc6ed6d06`
  （原版：`09214dbb6645b69051fa8d192600f880`）。这两个值随套件版本变化，
  不要拿它们当判据，看工具自己的输出。

## 许可

MIT，随便用，不担保。
