import { Page, Section, Text, Table, TableOfContents, Callout, NumberedList, CodeBlock, LinkCards, Checklist, Steps } from "grok/canvas";

export default function App() {
return <Page lang="zh-Hans" width="column" title="CyanDesk 软件设计原稿 v0.1（待审阅）" summary="面向个人多设备的免费开源自建远控系统：账号、穿透与中继竞速、高画质、虚拟分屏及独立 SteamVR 转发。本文是可执行设计基线，不是已实现功能说明；审阅后才交给 Cursor 开发。">
<Callout lead="设计状态 · 2026-11-23（北京时间）">全部 CyanDesk 功能均未实现、未实测。本轮只产出设计与官方资料核实，不写业务代码、不启动 Cursor/CloudAgent、不部署。此前开发已停止；按现有上下文仅有 seed README。2026-12-01 前拿到完整软件是用户目标，不是已确认交付承诺。</Callout>
<TableOfContents items={[
{label:"01 文档约定与产品边界",id:"s01"},{label:"02 全量需求追踪",id:"s02"},{label:"03 复用路线与许可证",id:"s03"},{label:"04 架构、模块与工程组织",id:"s04"},{label:"05 协议、端口与网络部署",id:"s05"},{label:"06 首次使用与会话状态机",id:"s06"},{label:"07 路径竞速与无感迁移",id:"s07"},{label:"08 身份、授权与安全模型",id:"s08"},{label:"09 数据模型与 API 契约",id:"s09"},{label:"10 客户端与后台交互设计",id:"s10"},{label:"11 媒体、画质与控制通道",id:"s11"},{label:"12 模拟分屏与无头 Linux",id:"s12"},{label:"13 平台能力与系统集成",id:"s13"},{label:"14 SteamVR 独立子系统",id:"s14"},{label:"15 Docker 安装与运维",id:"s15"},{label:"16 生成客户端、构建与更新",id:"s16"},{label:"17 测试环境与验收矩阵",id:"s17"},{label:"18 阶段目标与完整交付定义",id:"s18"},{label:"19 风险与待审阅决策",id:"s19"},{label:"20 交给 Cursor 的任务包",id:"s20"},{label:"21 官方资料与证据边界",id:"s21"}
]} />

<Section id="s01" heading="01 文档约定与产品边界">
<Table columns={["标记","含义"]} rows={[["用户要求 U","来自本次完整需求，不擅自删除或降为可选。"],["设计建议 D","为形成可执行方案而提出的默认值，用户尚未审定。"],["待验证 V","官方未保证、源码未核查或没有设备实测；不能作为支持声明。"],["完成状态","本文所有功能的实现与验收状态均为未开始；官方上游能力不等于 CyanDesk 已具备。"]]} />
<Text>产品目标：让个人拥有统一账号与设备列表，使用自有服务器，在设备允许的系统权限内安全远控自己的设备。优先接近 UU 的连接便利、操作连贯和画质体验，不复制其视觉、商标或私有实现。软件免费开源可修改，不依赖商业 Pro；云服务器、带宽、代码签名与 Apple 开发者资质可能有外部成本，不能承诺零成本运营。</Text>
<Text>完整范围同时保留桌面远控、Linux CLI、移动端、Pico、虚拟显示和 SteamVR。没有屏幕的 CLI 设备不能伪装成图形桌面；普通视频串流不能冒充 VR。建议附加文件传输、剪贴板作为独立可撤销权限，不把这些建议误写成用户已确认要求。任何阶段样机、未签名安装包或尚未通过的平台都不叫完整稳定版。</Text>
</Section>

<Section id="s02" heading="02 全量需求追踪">
<Table columns={["编号","用户要求","设计与验收索引"]} rows={[
["U01","免费、开源、可修改、自建","03、20；无 Pro 运行依赖；源码、依赖清单与许可证齐备。"],
["U02","账号制；首次填服务器地址＋账号密码","06、08—10；真实后端登录、设备归属、注销和撤销可测。"],
["U03","中继＋打洞同时尝试，快的优先","05—07；抓包证明并行候选、真实加密通道竞速与迁移。"],
["U04","一键 Docker 安装","15；干净服务器安装、恢复与升级回滚测试。"],
["U05","默认 13110 远控；13111 管理入口、生成/下载/版本","05、10、16；区分协议改造、预配包和原生构建。"],
["U06","系统集成、开机自启","13、17；逐 OS 验证服务、重启与授权边界。"],
["U07","不同画质；观看/控制模式；接近 UU 体验","10—11；会话中切换、主机权限强制、真实统计。"],
["U08","高画质与模拟分屏","11—12；分区布局、物理多屏、真正虚拟屏分别验收。"],
["U09","Windows 7/10/11 主控＋被控","13；Win7 独立遗留路线，不能算进现代统一包。"],
["U10","macOS Intel/Apple Silicon 主控＋被控","13；两种架构、系统授权与重启限制分别测试。"],
["U11","Linux CLI/GUI 主控＋被控","12—13；CLI 命令/文件与图形会话能力分开。"],
["U12","Android 9+、PicoXR 主控＋被控","13—14；桌面远控与 VR 接收是不同能力。"],
["U13","iOS/iPadOS/visionOS 仅主控，允许发布","13、16；安装/签名/审核路线验证，不承诺审核日期。"],
["U14","SteamVR 转发","14、17；头追、控制器、音频及真实双目串流独立验收。"],
["U15","2026-12-01 前拿到完整软件","18；目标排期非承诺；不以删功能或演示壳替代。"],
["U16","说完后先完整设计，再交 Cursor","20；当前只设计，用户审阅后才能启动实现。"]
]} />
<Text>变更规则 D：后续按 U 编号记录新增、修改、理由及影响；实现与验收列日后回填到同一 Page。不可静默把“不支持”改成“不在范围”。</Text>
</Section>

<Section id="s03" heading="03 复用路线与许可证">
<Table columns={["候选","结论","边界"]} rows={[
["RustDesk OSS＋自研控制面","推荐 D：复用远控引擎，独立完成账号、后台与策略","需要维护 fork；账号与单端口、竞速、桥接能力均不能只换品牌获得。"],
["完全自研远控引擎","保留后续路线，不建议作为首轮关键路径","跨系统采集、输入、编解码、弱网与权限工作量大。"],
["MeshCentral","账号管理可参考，不作为推荐主引擎","管理导向不等于 UU 式高画质与移动/VR 体验。"],
["HopToDesk Dashboard","不作为可直接复用的免费开源账号服务承诺","需逐项核查服务端源码和授权，本稿未采用。"],
["ALVR","推荐 D：独立 SteamVR 适配模块","MIT；不能推断现有二进制具有所需控制 API 或能无修改嵌入。"]
]} />
<Text>已核实：RustDesk 客户端与 OSS 服务端采用 AGPL-3.0；官方 OSS 提供 hbbs/hbbr，集中账号、Web console、API 与 custom client generator 位于 Pro 功能列表。本设计自行实现这些产品能力，不调用商业 Pro 的私有接口、代码或许可证服务。官方安装说明体现打洞失败后中继；本稿并行竞速是新增设计，不是对现有默认行为的描述。</Text>
<Text>合规 D/V：修改、分发或通过网络提供 AGPL 覆盖的软件时，遵循对应的源码提供、许可证和版权保留义务。是否构成衍生作品取决于代码复用、链接与组合方式，不能仅因 Go 服务独立进程就断言必须 AGPL 或必然不受影响。实现前建立组件边界与 SPDX/SBOM，由许可证审查确认。ALVR 的 MIT 声明与第三方驱动/音频/编解码依赖分别保留；不把额外收费依赖当成免费功能的默认前提。</Text>
<Text>不替用户决定公开仓库、商业授权或闭源发布。源代码提供给哪些对象、以何种合规方式交付，须按实际许可证义务及用户审阅结果确定；免费开源目标不等于本轮获得公开发布仓库的授权。</Text>
</Section>

<Section id="s04" heading="04 架构、模块与工程组织">
<Table columns={["模块","推荐技术与职责","不承担的职责"]} rows={[
["客户端 Shell","RustDesk Rust/Flutter fork；设备列表、会话、设置；Apple 特殊平台可独立原生包装 V","不保存服务器管理员权限，不绕过 OS 授权。"],
["Engine Bridge","Rust 核心＋明确 FFI/IPC 契约；封装采集、会话、传输与能力","不假定上游已有稳定可直接嵌入 API。"],
["Control API","Go；账号、设备、ACL、短期授权、版本、审计","不解密桌面/VR媒体，不替设备身份背书。"],
["Signal/Relay Gateway","Rust 推荐；改造 rendezvous/relay，多路复用与配额","不开放任意地址代理，不接收明文按键。"],
["Admin Web","Vue 3＋TypeScript；13111 下静态资源和管理 API","不通过 Web 请求运行任意构建命令。"],
["存储","单实例 SQLite WAL＋本地制品卷；小规模便于备份","多副本/多机前迁移 PostgreSQL 与共享对象存储。"],
["VR Adapter","ALVR 独立进程/模块＋受限本地控制桥 V","不把桌面 H.264 通道直接当头显 VR 通道。"],
["Build Workers","隔离、按 OS/架构部署的可信构建执行器","不挂载到公网管理服务的 Docker socket。"]
]} />
<CodeBlock maxHeight={360}>{`建议仓库模块树（设计，不创建仓库）
/docs/             ADR、威胁模型、协议、平台矩阵、验收
/client/           产品 Shell 与交互
/engine/           RustDesk fork 与变更补丁、bridge
/platform/         windows-modern、windows-legacy、macos、linux、android、apple-controller
/control-plane/    账号、设备、ACL、会话与版本 API
/gateway/          信令、中继、端口复用与配额
/admin-web/        管理后台
/vr/               ALVR 适配、头显能力、独立测试
/protocol/         schema、能力列表、兼容性测试向量
/packaging/        各平台安装与服务清单
/deploy/           Compose、迁移、备份、安装器规范
/build/            受限构建模板、签名与 manifest 规范
/tests/            网络故障、授权、端到端、设备测试记录`}</CodeBlock>
<Text>桥接契约 D：Initialize(ServerProfile)、Enroll(DeviceIdentity)、ListCapabilities、StartSession(SessionOffer)、ApplyPolicy、SetQuality、SelectDisplay、SetViewLayout、SendInput、CloseSession；事件为 PermissionRequired、StateChanged、FrameStats、PathChanged、FatalError。参数携带 request_id、session_id、protocol_version；错误至少区分 NOT_SUPPORTED、PERMISSION_REQUIRED、AUTH_REVOKED、VERSION_MISMATCH、NETWORK_TIMEOUT。命令必须幂等或定义重试规则。需源码验证后才能确定 FFI 还是有凭据校验的本机 IPC；privileged helper 只接受最小白名单操作，限制同机其他用户调用。</Text>
<Text>能力协商 D：每台设备公布 OS/架构、引擎版本、capture/input/audio、codec encode/decode、max_resolution/fps、displays、virtual_display、unattended、vr_send/receive；声明只做能力发现，真正授权由主机逐次强制执行。按会话取交集；不支持项返回明确原因，不静默假成功。各平台分别实现 capture/input/codec/service，禁止用“同一套 Flutter”推导同等系统能力。</Text>
</Section>

<Section id="s05" heading="05 协议、端口与网络部署">
<Table columns={["入口","协议与用途","真实性边界"]} rows={[
["13110/TCP","CyanDesk 信令＋加密中继流；TLS 后按协议帧类型复用","D：需要同时改客户端和网关协议；不是改 Docker 映射即可实现。"],
["13110/UDP","候选探测、地址发现、UDP 中继；QUIC 为优选待评估","D/V：鉴权、反放大、连接 ID 与拥塞控制均需实现和审计。"],
["13111/TCP","HTTPS：/admin、/api/v1、/downloads、/manifests","用户要求的管理员入口；客户端登录/设备 API 也接此端口，不仅后台使用。"],
["客户端临时端口","本地 UDP/TCP 候选、NAT 映射与直连","不可保证全网仅两个端口；防火墙需允许相应出站和已建立回包。"],
["VR 本地/额外端口","ALVR discovery/control/stream/audio 等","具体端口/传输按锁定版本源码及抓包核实；原生模式可能增加开放端口。"],
["内部服务","数据库/指标/构建队列仅内网或卷访问","不发布到公网；宿主机健康检查不等于开放管理端口。"]
]} />
<Text>对照：RustDesk 原生通常涉及 21115/TCP、21116/TCP+UDP、21117/TCP；WebSocket 还涉及 21118/21119。端口职责与精确开放清单在锁定版本验证。它不是原生只用 13110 的架构。迁移方案 D：先隔离环境验证引擎，再完成 CyanDesk Gateway 协议改造；若保留原生兼容模式，必须单独展示额外端口且不得按最终端口要求验收。</Text>
<Text>地址发现 D：首次填域名或 IP，可显式给端口；默认 HTTPS 13111 获取经认证的 server-info，返回 13110 transport endpoints、server_id、支持的协议与最小安全版本；不允许远端提供任意文件/命令。TLS SNI/ALPN 及帧头的最终设计须安全评审。建议控制 API REST/JSON v1，设备事件 WebSocket v1；媒体/输入二进制协议 cyandesk/1，schema 与产品版本独立，major 不兼容直接拒绝，minor 扩展忽略未知非关键字段。</Text>
<Text>网络 D：IPv4/IPv6 双栈、同网段候选、NAT 打洞、加密中继同时参与；对称 NAT/UDP 禁用时走 TCP 中继，不承诺所有网络可直连。UPnP 不默认启用；仅在明确授权下配置。13111 的 TLS 证书申请可能需要 DNS 验证或额外临时验证端口，与业务默认端口分开说明。VR 可选封装到既有加密 UDP 隧道，须测 MTU/分片、抖动、拥塞竞争与 Windows/Android 权限，未验证前不得宣传“两端口包办 VR”。</Text>
</Section>

<Section id="s06" heading="06 首次使用与会话状态机">
<Steps steps={[
{title:"1. 配置与验证服务器",detail:"首次启动填写服务器地址；校验证书/主机名与 server_id。失败停在可诊断状态，不能自动忽略证书。预配包可预填地址，但仍确认所属服务器。"},
{title:"2. 登录与设备登记",detail:"HTTPS 账号密码登录；在设备本机产生长期设备密钥，登记公钥、名称与能力。账号会话和设备凭据独立；新设备显示待批准状态。"},
{title:"3. 建立信任与权限",detail:"经已信任设备核对公钥指纹/扫码批准；首次唯一设备按离线 bootstrap 或本地人工核验建立根信任。用户明确设置允许的主控、观看/输入、无人值守和有效期。"},
{title:"4. 发起远控",detail:"主控选择设备与请求权限，API 进行 ACL 检查并发短期、绑定双端与权限的授权票据；主机在线同意，或依预设无人值守规则放行。"},
{title:"5. 并行连接与端到端认证",detail:"直连候选和中继同时启动；每条候选完成端到端握手、验证对端已信任设备密钥和会话随机数，再探测可用性。快的可用路径进入首画面。"},
{title:"6. 媒体与输入",detail:"协商显示器、编码和画质；视频/音频与可靠控制、输入逻辑隔离。只观看在主机输入注入层阻止输入。显示连接者、路径和断开操作。"},
{title:"7. 优化、断线与结束",detail:"后台继续短期评估候选，符合迟滞规则才迁移。断网释放按键并限时重连；结束撤销会话票据、清理中继和临时授权，写脱敏审计。"}
]} />
<CodeBlock>{`状态（D）
UNCONFIGURED → VERIFYING_SERVER → SIGNED_OUT → AUTHENTICATED
→ DEVICE_PENDING → READY → REQUESTING → WAITING_CONSENT
→ CONNECTING[DIRECT || RELAY] → PEER_AUTHENTICATED
→ STREAMING_VIEW / STREAMING_CONTROL
→ MIGRATING → STREAMING
→ RECONNECTING → STREAMING / CLOSED
任意阶段：证书/身份不匹配 → BLOCKED；权限撤销 → VIEW_ONLY 或 CLOSED
拒绝/超时/主机离线：保留明确错误，不自动降级安全校验。`}</CodeBlock>
<Text>建议初值 D，均未实测：同意窗口 30 秒、连接总预算 10 秒、首批候选握手预算 3 秒、心跳 5 秒、连续 3 次失联进入重连、重连窗口 30 秒，1/2/4/8 秒退避加随机扰动。是否允许会话恢复须检查撤销、设备密钥、主机权限和短期恢复令牌；不能仅凭旧 session_id 恢复输入。</Text>
</Section>

<Section id="s07" heading="07 路径竞速与无感迁移">
<NumberedList items={[
"D：直连与中继尽量在同一调度周期开始，记录启动时间；不先等待打洞失败。中继候选建立须通过授权与配额检查，避免为了竞速成为开放代理。",
"D：候选可用定义为认证成功的端到端加密通道＋短时探测成功，不是 TCP 握手或 ping 最小。采样 RTT、抖动、丢包、可用吞吐和首帧时间；带宽探测限速限时，不把所有候选同时拉到满速。",
"D：先选达到最低媒体门槛且最早可出图的路径，尽快出首画面；其余路径仅探测，不重复启动全量视频。之后按当前画质所需吞吐门槛、丢包和抖动过滤，再比较输入延迟与稳定性，避免机械取最低 RTT。",
"D：切换建议阈值为连续 3 秒综合改善 20% 或当前路径已失效，冷却 10 秒；所有阈值未实测、需网络测试校准。关键输入优先，不能为了提码率牺牲交互响应。",
"D：会话 ID 与 transport ID 分离；迁移使用已认证密钥派生新路径子密钥和独立计数器，不复用 nonce。候选切换确认后发关键帧，按媒体时间戳去重；输入使用 session_epoch＋单调序号，主机去重并确认，绝不向两条路径重复注入一次点击。",
"D：旧路径短暂保活只承载受控恢复，不重复媒体；新路径确认后释放旧路径带宽，停止无用探测。按键断线/焦点变化发送 release-all，避免卡键；切换失败回到仍可信的旧路径，否则进入重连。",
"D：每账号/设备/会话有带宽、并发、探测预算和流量上限；票据短期有效、绑定双方公钥与会话随机数，禁止任意目标地址转发。UDP 先验证源地址 cookie，回复不放大；失败鉴权不得分配昂贵媒体资源。"
]} />
<Text>验收关键证据：直连与中继并行时间线；每候选的认证/探测结果；选路理由；切换期间媒体间断、输入序号；未选中路径的资源释放。界面可手动固定直连优先/中继或恢复自动，但不能让用户误以为“直连”总是更快。</Text>
</Section>

<Section id="s08" heading="08 身份、授权与安全模型">
<Table columns={["层级","设计建议","防护边界"]} rows={[
["服务器身份","公有 CA 域名证书优先；IP 须有匹配 IP SAN 的证书","不得关掉主机名校验。自签证书仅显式固定指纹；TOFU 首次连接仍可能被中间人攻击，应离线核对。"],
["账号身份","Argon2id＋独立随机盐，参数按服务器压测；管理账号建议 TOTP","密码不明文/可逆存储；限速同时按账号、来源和全局，错误消息避免枚举。"],
["设备身份","本机长期密钥＋已信任设备批准/指纹固定；安全存储","服务器签的 ticket 仅代表授权，不能单独证明 peer 身份。服务端被攻陷也不能静默替换已固定设备公钥。"],
["会话安全","成熟、可审计的 Noise/TLS 类端到端库及临时密钥交换 V","不自创密码算法；绑定双方身份、协商参数、权限、随机数和协议版本，防重放/降级。"],
["主机授权","owner、授权主控、watcher、admin 分离；最小权限","admin 能管理账号/撤销设备，但没有自动观看全体用户屏幕的权限。"],
["本地特权","系统服务与用户采集进程隔离；受限 IPC","不默认 root 跑完整 UI；输入/驱动安装权限单独授予。"]
]} />
<Text>信任 D：同账号不自动获得所有设备无人值守权。新主控须在已信任设备确认；允许按“设备组＋权限＋期限”授权。恢复账号或管理员重置密码不自动恢复设备加密信任；换钥进入待核验，可由原钥签署轮换并获本地确认，否则人工指纹重绑。全新设备且没有旧信任时，必须明确本地建立信任，不能以服务器可签发 ticket 为理由跳过。服务端受侵仍可能阻断连接、泄露元数据或推送恶意更新，因此更新签名密钥与管理面隔离。</Text>
<Text>凭据 D：建议访问令牌 15 分钟、旋转刷新令牌最长 30 天（目标默认，待审阅）；只存刷新令牌哈希，发现重用撤销整条令牌族。注销撤销当前会话，全部注销递增 token_epoch；撤销设备立即停止其新授权并推送结束现有连接，离线主机恢复时必须重新检查。长期设备密钥存 Windows DPAPI、macOS/iOS Keychain、Android Keystore；Linux 优先 Secret Service，headless 用最小权限密钥文件并提示静态磁盘风险，Win7 存储/算法支持另验。</Text>
<Text>隐私 D：不默认录屏、录音或记录按键/剪贴板/密码；审计保存身份、时间、授权结果、路径和字节计数。建议保留 30 天可配置，最小化 IP 等元数据；审计事件追加写并做完整性校验，但不承诺能抵挡拥有宿主机 root 的攻击者。账户删除、制品删除与日志清理由显式管理操作执行，不在安装/卸载时暗中删除数据。</Text>
<Text>安全默认 D：不允许明文降级；剪贴板、文件、音频、输入、远程命令分别授权。观看可降权无需重连，升为控制须主机重新批准。文件访问限制根目录、防路径穿越；CLI 命令白名单与高危操作本地审批。主机明显显示活动连接且随时可断开，无隐身远控设计。</Text>
</Section>

<Section id="s09" heading="09 数据模型与 API 契约">
<Table columns={["实体","最小字段（设计，无真实个人数据）"]} rows={[
["users","id、login_name(unique)、password_hash、role、status、token_epoch、created_at"],
["device_identities","id、owner_id、name、os、arch、public_key、fingerprint、key_version、trust_state、capabilities_json、last_seen、revoked_at"],
["device_grants","id、host_id、controller_id/group_id、scopes、unattended、expires_at、approved_by、policy_version"],
["auth_sessions","id、user_id、device_id、refresh_hash、family_id、expires_at、revoked_at"],
["remote_sessions","id、host_id、controller_id、requested/granted_scopes、state、ticket_jti_hash、started/ended_at、path_summary、bytes、close_reason"],
["releases / artifacts","release_id、channel、version、min_safe_version、engine/protocol_version；artifact_id、os、arch、size、sha256、signature、signing_key_id、status"],
["build_jobs","id、template_id、source_commit、server_profile_id、target、status、runner_id、artifact_id、failure_code、created_by"],
["audit_events","id、actor_id、action、target_id、result、request_id、timestamp、redacted_metadata、integrity_hash"],
["relay_quotas / server_profiles","account_id、rate/concurrency/bytes_limit；server_id、public_endpoints、certificate_fingerprint、config_version"]
]} />
<Text>所有时间存 UTC、展示转换为北京时间；这里的设计日期均为北京时间。数据库启用外键、事务、唯一索引与迁移版本；设备撤销、授予权限和生成票据使用一致性检查，避免竞态。设备在线状态来自短 TTL 的心跳，不把数据库中的 last_seen 误作当前在线。</Text>
<Table columns={["端点 D","核心契约"]} rows={[
["GET /api/v1/server-info","仅公开最小元数据：server_id、endpoints、api/protocol_versions、min_safe_client；必须经 TLS 验证。"],
["POST /api/v1/auth/login、/refresh、/logout","登录返回短期令牌与到期时间；refresh 单次轮换；logout 撤销。后台浏览器用 Secure/HttpOnly/SameSite Cookie＋CSRF 防护，不把令牌放 URL。"],
["POST /api/v1/devices/enroll","公钥＋签名挑战＋平台能力；返回 pending 与设备 ID，绝不因登录成功直接赋予无人值守。"],
["GET /api/v1/devices；POST /devices/{id}/approve、/revoke","仅列可见设备；approve 绑定指纹确认，revoke 要权限和审计；越权对象统一拒绝。"],
["PUT /api/v1/devices/{id}/grants","owner 修改 scopes/期限/无人值守；使用 policy_version / If-Match 防覆盖，更新即时下发。"],
["POST /api/v1/sessions","host_id、controller_id、requested_scopes、nonce、capability_offer；返回 request_id、consent_required，不在同意前签可用媒体票据。"],
["POST /sessions/{id}/consent；DELETE /sessions/{id}","host 签名确认 granted_scopes；双方均可断开；会话授权绑定双方公钥、过期时间、一次性 jti。"],
["WSS /api/v1/events","经认证设备事件、信令通知与撤销；事件有序号，可恢复游标；不得传裸密码或媒体解密密钥。"],
["GET /api/v1/releases/check；GET /downloads/{artifact_id}","按 OS/架构/版本返回已签 manifest；私有包短时下载授权；访问需遵守设备与发布策略。"],
["/api/v1/admin/*","users、devices、sessions、quotas、releases、build-jobs、audit；每项权限独立；不接受任意 shell、仓库 URL 或上传脚本执行。"]
]} />
<Text>共同规范 D：request_id 贯穿；写请求 Idempotency-Key；游标分页与大小限制；400 参数、401 失效身份、403 无权、409 版本/幂等冲突、429 限流、503 暂不可用。日志不得含密码/令牌/私钥。API 不直接信任客户端上报 owner_id；从认证上下文推导身份。会话列表只提供允许查看的元数据，admin 不返回屏幕预览。</Text>
</Section>

<Section id="s10" heading="10 客户端与后台交互设计">
<Table columns={["页面","信息与操作"]} rows={[
["服务器与登录","服务器地址、证书/指纹详情、账号登录及明确错误；只有文档描述，不在本 Page 制作登录表单。更换服务器隔离账号、设备信任与缓存，需确认，不悄悄上传原有设备。"],
["设备列表","我的/获授权设备，在线状态、平台、最近使用、允许能力；连接按钮选择观看或控制；新设备待批准与主机离线清晰区分。"],
["远控会话","画面为主体；工具栏：断开、观看/控制、画质、显示器、原图/适应/全屏、键盘/触控、连接统计；移动端工具栏可收起但不隐藏退出。"],
["设备权限","主机侧授予对象、观看/输入/音频/文件、无人值守开关与期限；系统授权说明、自启状态、撤销设备、当前连接；默认不启无人值守。"],
["网络诊断","直连/中继、候选结果、RTT、抖动、丢包、码率、编解码器、实际帧率；可导出脱敏诊断，先预览再由用户决定分享。"],
["更新与关于","当前版本、签名状态、可用更新、回退条件、组件许可证与源码获取；不把未验证包标记稳定。"],
["CLI","server/login/enroll/devices/connect/status/permissions/update 等命令语义；交互输入秘密不落命令行历史。connect 明确选择命令、文件或启动图形查看器，不伪造终端内桌面。"]
]} />
<Text>交互 D：首次使用最短主路径为“地址 → 验证服务器 → 登录 → 绑定设备 → 选择设备”；账号密码不取代主机系统授权。连接状态有可取消进度和真实阶段；失败只给相关下一步，不无限转圈。鼠标/触控两套映射可切换；缩放与坐标变换必须同步 DPI、多屏原点、旋转，防点错位置。键盘组合键在远端与本地间明确分配，焦点离开释放修饰键。</Text>
<Text>观看模式＝禁止远端输入；原图/适应/全屏＝客户端画面布局，两者彼此独立。主机拒绝控制后，工具栏不能通过 UI 开关绕过。被控端应持续显示连接者与可用的立即终止入口；系统平台无法覆盖安全桌面时展示限制，而不是伪造黑屏连接正常。</Text>
<Table columns={["后台分区","管理职责与限制"]} rows={[
["账户/设备/会话","创建/禁用账号、角色、设备撤销、会话元数据与终止；管理员不自动取得设备控制权。"],
["中继与策略","账号/设备限额、并发、探测预算、端点状态、流量统计；权限更改有审计。"],
["版本与下载","按平台/架构/渠道发布、上传校验、签名状态、兼容矩阵、下载与撤回；下载页是否公开须审阅。"],
["生成客户端","配置包任务与 native 构建任务分开展示；输入只允许审核过的参数，显示构建/签名/测试三种独立状态。"],
["审计与运维","登录/授权/发布/构建事件、迁移/备份状态；脱敏，不显示密钥、密码或录屏。"]
]} />
</Section>

<Section id="s11" heading="11 媒体、画质与控制通道">
<Table columns={["建议预设 D","分辨率 / FPS / 码率","用途"]} rows={[
["省流","720p / 15–30 / 1–3 Mbps","弱网与基础操作"],
["均衡","1080p / 30 / 4–8 Mbps","默认起点，随后自适应"],
["高清","1080p / 60 / 10–20 Mbps","支持硬件与链路时启用"],
["超清","1440p–4K / 30–60 / 20–60 Mbps","按实际显示器、编码器、温度与带宽协商"],
["自定义","用户上限＋安全范围内的 FPS/分辨率/码率","不承诺任意设备都能实时达到参数"]
]} />
<Text>以上全部为建议预设/设计目标，未经实测，不是最低带宽或画质保证。优先 H.264 广兼容配置（Baseline 为候选保底），实际 profile/level、色度与硬件支持协商；HEVC/AV1 可选，不能以支持解码推导支持编码。软件编码/解码为降级路径，可能需降帧降分辨率；Win7 与移动端逐机验证。文字模式可提高文字清晰度，但 4:4:4、无损或 HDR 不作为已具备功能，需明确协商和额外预算。音频编码推荐 Opus，Android 内部音频采集还受系统版本及应用策略限制。</Text>
<Text>流水线 D：capture → 可选缩放/脏区分析 → 硬/软编码 → 受控队列 → 加密传输 → 解码 → 渲染。媒体队列积压优先丢过时帧并请求关键帧，不堆积秒级延迟；可靠控制/授权与输入优先于批量文件。UDP/QUIC 不支持时可 TCP，但必须显示队头阻塞与性能降级。带宽自适应结合丢包、RTT、渲染和编码队列，不只看下载速度。切画质重新协商并安全重配编码器，不能重置设备授权。</Text>
<Text>统计 D：展示采集/编码/解码/渲染 FPS、码率、分辨率、编解码器、路径、RTT、抖动与估算丢包；端到端延迟须说明测法，不能把 RTT/2 当实测画面延迟。遥测默认仅本地/自建端，不能上传第三方。VR 有独立预算与统计，不沿用桌面预设。</Text>
</Section>

<Section id="s12" heading="12 模拟分屏与无头 Linux">
<Table columns={["层级","设计","验收边界"]} rows={[
["L1 观看布局分区","同一远端画面分为若干裁剪区/独立窗口；可缩放平铺","不增加 OS 显示器；应用仍看到原有桌面。输入坐标映射与每区焦点须准确。"],
["L2 已有多显示器","枚举真实显示器、单屏/全屏组/多窗口观看、分辨率与 DPI 同步","不等于虚拟显示器；插拔、旋转、不同 DPI 与负坐标均需测试。"],
["L3 真正 OS 虚拟显示","创建额外显示器，让远端应用可把窗口放到扩展桌面","独立平台实现，需驱动/系统权限与签名；不能承诺全平台一致。"]
]} />
<Text>Windows D/V：Win10/11 评估 IddCx/Indirect Display Driver 与签名流程；具体最低系统版本及驱动路径按 WDK/设备验证。Win7 无法直接沿用现代 IddCx，需独立遗留显示驱动或其他合法适配路线，安全与兼容性均高风险，不能把裁剪窗口冒充 Win7 虚拟显示。虚拟屏安装/卸载需明确管理员确认；断连可配置保持/移除，异常退出要有本地恢复工具，避免用户失去可见桌面。</Text>
<Text>macOS D/V：不得依赖未公开私有接口声称万能创建虚拟屏；评估受支持显示扩展路径或明确外部依赖。Linux D/V：X11 可评估独立虚拟 X 会话，Wayland 取决于 compositor/portal 支持；这不一定能扩展当前物理登录桌面。Android/iOS/visionOS 作为观看端可做 L1/L2 展示，不能推导出被控系统支持 L3。各平台 L3 都保留在目标中，未验证就标阻塞，不改名为已完成。</Text>
<Text>Headless Linux D：CLI 被控默认提供有目录与命令限制的管理通道，远程 shell 是否放开需显式授权；建议优先复用成熟 SSH 安全机制或受限命令执行器，而非自创 shell 协议。可选部署独立虚拟桌面会话后才能采集 GUI，它与实体登录会话分开标识。CLI 主控可列设备、授权、诊断、管理文件/命令；查看远程 GUI 需要图形查看器或明确启动图形会话，纯终端不能宣称同等桌面交互。</Text>
</Section>

<Section id="s13" heading="13 平台能力与系统集成">
<Text>下表是目标与风险矩阵，不是支持清单。所有 CyanDesk 平台都未构建/未验收；“目标双向”指用户要求主控＋被控，不能由上游支持直接视为完成。</Text>
<Table columns={["平台 / 方向","无人值守与自启","显示/签名/限制"]} rows={[
["Windows 10/11 / 双向目标","安装服务后自启，用户 UI 与系统服务分离；锁屏/UAC/登录界面需逐项验证","L1/L2＋L3 驱动路线；x64/ARM64 等按实际设备确认，服务/驱动与安装包签名分开。"],
["Windows 7 / 双向目标","遗留服务分支；TLS、信任根、系统补丁与输入权限另验","独立 legacy 工具链/运行时/可能不同 Shell；现代 Flutter/Rust 不保证支持。旧版依赖的安全维护是阻塞项；不承诺同一个构建包。L3 另研；ALVR 主机不支持。"],
["macOS Intel / 双向目标","登录项/LaunchAgent/受限 helper；屏幕录制与辅助功能本地授权","按系统版本验证采集/输入；签名＋公证。FileVault 重启后的预启动解锁不能承诺无人值守远控。L3 未验证。"],
["macOS Apple Silicon / 双向目标","同上；两架构独立测试，可评估 Universal 包","原生 arm64 与依赖 ABI 验证；不能把 Rosetta 跑通当原生支持。ALVR macOS 主机不支持。"],
["Linux X11 GUI / 双向目标","systemd 设备代理＋用户会话采集；安装权限明确","发行版/桌面/显卡、登录界面与音频分别验。deb/rpm/AppImage 等目标包按设备确定；L3 评估虚拟会话。"],
["Linux Wayland GUI / 双向目标","portal/PipeWire 与 compositor 政策；可能需每会话授权","RustDesk 文档仍标实验支持；登录界面需 X11。不能承诺无交互无人值守或登录界面远控。"],
["Linux CLI / 双向目标","systemd agent 可重启恢复；受限命令/文件权限","无 GUI 不采集不存在的屏幕；图形主控需另起查看器。CPU 架构和发行版 libc 兼容待设备清单确认。"],
["Android 9+ / 双向目标","前台服务、可见通知、自启与省电例外引导；不绕过 MediaProjection/Accessibility","重启/服务重建后录屏授权受版本限制；新 Android 的 token/前台服务规则另验。Android 9 内部音频不可按 10+ 能力承诺；APK 签名，商店审核另计。"],
["PicoXR / 双向目标","Android 桌面被控需 ROM/OEM 实测；头显待机和系统权限单独处理","桌面采集不能保证看到受保护/合成 VR 画面。Pico 型号、系统版本与 OpenXR/控制器映射需核实；APK sideload 依设备政策。"],
["iOS/iPadOS / 仅主控","不提供被控服务、不承诺后台常驻；前台恢复连接","触控、键鼠/手柄适配按 API 验证；签名 IPA，TestFlight/App Store/开发签名受资质与审核限制。"],
["visionOS / 仅主控","前台远程窗口与焦点/手势设计；无被控、自启服务承诺","RustDesk fork 是否可直接编译未验证；可评估 SwiftUI＋核心桥接。ALVR 官方列 Apple Vision Pro 客户端，但不等于 CyanDesk 已集成或任意 visionOS 设备都兼容。"]
]} />
<Text>系统集成 D：桌面托盘/菜单栏、开机服务状态、网络变化重连、休眠/唤醒、权限失效提示、卸载清理分离。建议默认只启动设备代理，是否自动允许被控由独立权限决定；手机和 Apple 移动系统不把“安装了软件”等同于“重启后静默可被控”。Linux 登录界面方案不得自动改用户显示服务器，需明确告知影响并授权。</Text>
<Text>Apple 安装 D/V：开发签名适合少量自有设备，受证书/设备注册/有效期约束；TestFlight 受开发者资质、构建处理及测试规则限制；App Store 受审核与政策限制；其他分发方式只在资格与地区政策允许时评估。不收集 Apple 密码，不承诺 12 月 1 日前完成商店上架。所需账号、设备版本和签名资源在设计批准后再按安全流程确认。</Text>
</Section>

<Section id="s14" heading="14 SteamVR 独立子系统">
<Text>真实链路 D：受支持的游戏 PC 上运行 SteamVR＋ALVR streamer/driver → 编码双目 VR 帧并传给 Pico 等头显 → 头显本地解码与呈现；头部姿态、手柄位姿/按键/触发器回传 SteamVR，同时处理声音与可选麦克风。必要时序、预测、帧节奏及重投影由适配的 VR 栈负责。桌面远控只辅助启动、设置和维护，不能替代 VR 流。</Text>
<Table columns={["事实 / 决策","设计边界"]} rows={[
["已核实：ALVR MIT","保留版权和许可证；依赖/驱动单独检查。官方 PC 支持 Windows 10/11 与 Linux，不支持 Win7/8 或 macOS 主机。"],
["已核实：头显清单","官方 README 列 Pico Neo 3/4/4 Ultra、Apple Vision Pro；Android/Monado 标有限测试。不能由 Android 9+ 推导所有 Android 都是合格 VR 接收端。"],
["待验证：设备 SDK/运行时","锁定 ALVR 版本，核查 Pico 客户端采用的 OpenXR/厂商接口、机型差异、控制器绑定；不泛称任意 OpenXR 即可兼容。"],
["D：控制与媒体隔离","账号统一发现设备和授予 VR 会话；ALVR pairing/trust 与版本匹配保留，进程控制桥仅监听本机且鉴权；不直接暴露 Dashboard 到公网。"],
["D/V：网络模式","先局域网原生验证；再显式配置远端地址/路由，随后评估 CyanDesk 加密 UDP 隧道/专用中继。发现广播不等于跨 NAT 可用。"],
["V：端口与加密","官方跨网络 Wiki 链接本轮未获得有效正文，故不伪造端口列表；需锁定版源码/抓包确认 discovery/control/stream/audio 端口及动态范围、原生加密与公网暴露面。未确认前不直接开放公网。"]
]} />
<Text>VR 体验目标 D，未实测：先以 PC 有线千兆＋头显同路由 5GHz/6GHz Wi-Fi、受支持 GPU 硬编验证 72/90Hz 与约 30–100 Mbps 可调档，再按实机分辨率、编码器与温度调优；不得把这些数值当最低配置或稳定性保证。跨公网再记录上行、RTT、抖动、丢包、motion-to-photon 测法及主观舒适性。公网延迟可能导致不适，不承诺公网达到本地 PCVR 体验。</Text>
<Text>路由与资源 D：VR 与桌面共用服务器时设独立队列/限额，优先保留控制与姿态数据，禁止文件下载压满链路。跨公网使用成熟加密隧道或经验证的安全传输，不假定 ALVR 原生连接等同 CyanDesk E2EE。若隧道额外权限/端口不可避免，必须在安装器说明并由用户审阅；Docker 服务端不能替用户 PC/头显安装 SteamVR 驱动或授予系统权限。</Text>
<Text>独立验收：双目画面、头追六自由度、左右手柄与按键、音频、断连安全回退、PC/头显版本匹配、LAN 与 WAN 分别记录。Pico 能作为 Android 被控端，并不表示其 VR 层能被普通远控注入；visionOS 的桌面主控与 ALVR VR 接收也分别实现和验收。不以 Win7/macOS 能跑 CyanDesk 主控推导它们能当 ALVR SteamVR host。</Text>
</Section>

<Section id="s15" heading="15 Docker 安装与运维">
<Table columns={["Compose 服务 D","部署与数据"]} rows={[
["edge-admin","13111 HTTPS；静态后台、API/下载反代；不信任公网直接传入的 X-Forwarded-*，只信任自有反代。"],
["control-api","非 root、内网访问；数据库连接/令牌签发；只持有必要控制面秘密。"],
["gateway","13110 TCP＋UDP；信令/中继，资源限额；容器外网策略受限，禁止任意代理。"],
["maintenance","按需备份、迁移和制品完整性检查，不常驻暴露端口。"],
["volumes","db、artifacts、server-config、audit 分卷；证书与 secret 独立只读挂载；构建 worker 不在公网 Compose 中。"]
]} />
<Steps steps={[
{title:"安装前检查",detail:"确认 Linux/Docker/Compose 版本、CPU 架构、公网/转发条件、域名/证书、磁盘与端口冲突；最小服务器资源按中继带宽压测再定，不照搬上游宣传容量。"},
{title:"可信一键安装",detail:"发行安装器、Compose 与镜像 digest；先校验可信发布渠道的签名/hash并查看动作，再运行安装器。不使用盲目 curl | sudo。只为明确的宿主机操作请求权限。"},
{title:"初始化",detail:"生成随机 secret 与一次性短效 bootstrap 凭据，经本地受保护通道交付，首次必须设置管理员凭据；无固定默认密码，安装日志/健康接口不输出 secret。"},
{title:"部署自检",detail:"检查持久卷权限、TLS、13110 TCP/UDP 和 13111 可达；创建测试账号完成真实设备会话才算安装成功，不以容器 running 代替远控验收。"},
{title:"升级与恢复",detail:"先做数据库一致性备份与制品/配置/信任资料备份，再执行版本化迁移；预检客户端兼容性。回滚应用要匹配 schema，破坏性迁移用备份恢复，不盲降版本。"}
]} />
<Text>备份 D：建议每日备份数据库、配置、公钥信任和制品索引，备份加密且保存于另一故障域；私钥是否备份需单独策略，恢复后必要时轮换。SQLite 使用一致性备份接口，不能复制活跃中的单个 db 文件就宣称可靠。目标 RPO 24 小时/RTO 1 小时均待实际规模压测。每次正式发布前演练一次空机恢复。</Text>
<Text>观测 D：/health/live 与 /health/ready 分离；监控账号限流、登录异常、会话失败原因、直连率、relay 带宽/队列、磁盘、证书过期和构建失败。健康接口只暴露最小状态，详细指标经授权内网读取。Docker 自动重启策略和日志轮转；禁止 privileged 容器、host root 文件挂载、明文 secret 入镜像。</Text>
<Text>卸载 D：默认停止并移除服务，保留数据卷、备份、信任材料和制品；删除数据必须是独立、明确、可确认操作。客户端卸载分别清理服务、驱动和本机凭据，不擅自撤销其他设备；服务端停机不会自动清除对端设备信任。</Text>
</Section>

<Section id="s16" heading="16 生成客户端、构建与更新">
<Table columns={["类型","输出与限制"]} rows={[
["A 预配配置包 D","现有可信客户端＋签名配置（服务器地址、server_id、公钥指纹、渠道、品牌配置）；不内嵌账号密码、管理员 token 或设备私钥。应明确称“预配包”，不冒称重新编译的软件。"],
["B 原生构建 D/V","按已批准源码 commit、目标 OS/架构、受限品牌/服务器配置生成实际二进制；分别经历构建、签名、公证/审核、设备测试。只有真实制品才显示可下载。"],
["Apple 构建 V","需要合适的 macOS/Xcode 构建资源、证书、provisioning 与资质；单个 Linux Docker 不能跨出所有已签名 Apple 包。"],
["Win7 legacy V","独立兼容分支与安全维护策略；旧工具链可复现不等于仍有安全支持；失败必须明确阻塞，不下载 Win10 包凑数。"]
]} />
<Text>构建安全 D：公网后台只提交 template_id、固定 source_commit、target 与白名单配置；不接受 arbitrary shell、任意 Dockerfile/URL 或未审查代码执行。worker 按平台隔离、无宿主机 Docker socket、一次任务一沙箱；拉取源码与依赖固定版本，记录 SBOM/构建来源。上传制品先隔离扫描、验证大小与 hash，再由受控签名阶段签名；签名密钥不交给一般构建任务，管理面和更新签名权限分离。</Text>
<Text>manifest D：product、version、channel、OS、arch、min_os、api/protocol_range、engine/VR_version、artifact_size、sha256、signature、key_id、min_safe_version、published_at、source_commit。客户端先验证离线固定的发布公钥/受信轮换链、版本与 hash，再安装；TLS 不能替代制品签名。更新签名与平台 Authenticode/Apple/APK 签名各有用途，两者都要按平台验证。</Text>
<Text>回退 D：仅允许回到兼容配置/数据库且不低于 min_safe_version 的已签名版本；阻止重放过期 manifest 与降级攻击。紧急回滚须有签名授权与用户确认，不默认关闭版本防护。普通客户端可分阶段发布、下载不中断会话，安装前提示；驱动升级与重启需单独确认。密钥轮换和泄露处置在首个可用自动更新版本前完成演练。</Text>
</Section>

<Section id="s17" heading="17 测试环境与验收矩阵">
<Text>测试基线 D：真实用户设备清单（型号、OS 版本/架构、GPU、驱动、显示器、Pico/Apple 具体机型）待设计批准后确认。桌面参考环境建议 Win11＋支持硬编 GPU、1080p/60Hz＋4K 多屏；其他目标平台均需真机或合格虚拟机安装验证，GPU/驱动/VR 权限相关必须真机。Linux 至少 X11、Wayland 与 headless 三类。Win7 与 Apple 的具体最低版本、架构不能靠此文假定。</Text>
<Table columns={["验收组","测试与通过证据"]} rows={[
["A 账号与绑定 U01/02","干净服务器安装、不同账号登录；无权设备不可见/不可连；新设备待批准；注销、刷新重用、禁用账号、撤销设备即时生效；日志无秘密。"],
["B 连接策略 U03","LAN、双层 NAT/对称 NAT、IPv6、UDP 禁用、仅 TCP 中继；记录并行启动、端到端认证与探测；无法直连仍能有权限地走中继。"],
["C 迁移与输入","控制中断开首选路径、网络切换、Wi-Fi 唤醒；无重复点击/卡键/未授权恢复，媒体迁移失败可恢复或明确断开；中继资源及时释放。"],
["D 画质与弱网 U07/08","每个预设和协商回退；2/5/20/100 Mbps、RTT 20/80/150ms、丢包 0/1/3%、抖动 0/20/50ms 的受控组合；记录真实 FPS/码率/延迟，不把设置值当输出值。"],
["E 权限与攻击面","只观看输入注入被主机拒绝；管理员不能默看；换钥/MITM/票据重放/降级/越权/路径穿越被阻止；无票据 relay 与 UDP 放大测试通过。"],
["F 显示 U08","L1 分区、L2 物理多屏、L3 真虚拟屏分别证明；DPI/旋转/热插拔、headless 与驱动卸载恢复；不以观看窗口代替 OS 扩展屏。"],
["G 平台与自启 U06/09—13","每个承诺的平台双向/仅主控范围逐项安装；重启、登录前后、锁屏、休眠、权限撤销/恢复；Android/Pico 重启后授权行为如实说明。"],
["H VR U14","受支持 PC→指定 Pico；双目、头追、手柄、音频、帧时序、LAN/WAN、断线退出与版本匹配；记录舒适性与性能，普通桌面视频不算通过。"],
["I 发行与运维 U04/05","13110/13111 实际监听、额外端口清单；预配/原生构建区分；签名验证、篡改拒绝、失败回滚、备份恢复、卸载保留数据和安装文档复现。"]
]} />
<Text>建议性能目标 D，未实测且待用户确认：参考 LAN 1080p60 硬编环境，首画面 p95≤3 秒、输入到画面 p95≤100ms、连续会话 2 小时无崩溃；跨路径迁移可见停顿 p95≤1 秒，且重复输入为零。公网只给指定网络条件的测量结果，不做无条件 SLA。VR 先验证设备原生刷新档稳定性与帧时序，再确定舒适延迟预算；不得用桌面指标替代 VR 指标。</Text>
<Text>报告规范 D：每条案例记录 build hash、设备/驱动/网络配置、日期（北京时间）、预期/实际、日志/抓包脱敏路径、通过/失败/阻塞与复测结果。没有设备、签名或实测证据即标未验收；不得把“能启动”当“能稳定被控”。安全关键失败阻止发布稳定渠道。</Text>
</Section>

<Section id="s18" heading="18 阶段目标与完整交付定义">
<Callout tone="warning" lead="排期说明">用户目标是 2026-12-01 前拿到完整软件。以下是 2026-11-23 至 11-30（北京时间）的验证优先目标排期，不是承诺；只有用户审阅设计并明确启动后才进入执行。全平台、Win7、驱动签名、Apple 发布与公网 VR 可能使完整交付无法在该目标日前达成，不能通过删功能或改称“完成”掩盖。</Callout>
<Table columns={["目标日期（北京时间）","阶段 / 出口","阻塞时处理"]} rows={[
["11-23","G0：设计审阅；冻结 U/D/V 边界、设备清单和成功定义","未获审阅不启动代码；后续日期随实际开工顺延。"],
["11-24","G1：锁定上游版本/许可；桥接、Win7、Pico、Apple、VR 与端口最小验证","必须回报可行/不可行证据；不能未经验证直接铺开全平台。"],
["11-25—26","G2：账号/设备/权限、安装器、首条真实桌面远控闭环","样机必须真登录、真输入/视频；不接受静态 UI 和假数据代替。"],
["11-27","G3：并行竞速/迁移、高画质、系统服务与多屏验证","单端口改造和端到端身份验证不过，不称满足核心网络目标。"],
["11-28","G4：移动/Apple/legacy 适配，L3 虚拟屏与 LAN VR 的并行验证目标","平台能力不足逐项登记阻塞，不从完整范围中删除。"],
["11-29","G5：WAN VR、弱网/安全回归、包签名与原生构建验证目标","依赖真实设备、资质、网络与签名资源；外部审核无法控制。"],
["11-30","G6：全矩阵回归、恢复演练、安装包与文档整理、差距清单","仅全部门槛通过才可称完整稳定；否则明确交付候选/验证版与未完成项。"]
]} />
<Text>关键路径 D：审阅 → 上游/许可证/平台可行性 → 引擎桥接与真实远控 → 双端身份与账号授权 → 并行竞速/13110 改造 → 平台适配 → 签名/安装 → 全量验收。VR 与虚拟屏可并行，但不因核心远控通过就视为完成。Win7 legacy、Wayland 无人值守、macOS 虚拟显示、Apple 分发和 ALVR 适配均可能成为独立硬阻塞。</Text>
<Text>完整交付定义 D：全部 U 项有可安装可复现的实现与测试证据；各目标系统方向正确；自建真实账号与后台；要求的网络策略、画质、观看/控制、模拟分屏和 VR 均按定义通过；源码/许可证/构建说明、签名制品、Compose 安装、升级回滚、备份恢复和已知限制文档齐备。驱动与系统权限依法获得；不能用 CLI 管理模拟 GUI 支持、未签名包模拟可安装发行、LAN 演示模拟公网稳定。若客观系统限制与“所有平台完全相同”冲突，须回到设计审阅，不擅自视为用户接受缩减。</Text>
</Section>

<Section id="s19" heading="19 风险与待审阅决策">
<Table columns={["风险","影响与验证动作"]} rows={[
["期限 / 范围","8 天窗口不足以可靠承诺全平台稳定；优先证伪高风险，公开差距，不压缩安全验收。"],
["Win7 legacy","现代运行时/工具链/签名/TLS 与驱动兼容冲突；先做采集、输入、编码、加密与安装探针，再决定维护方案。"],
["单端口与竞速","需要改协议与引擎，非端口转发配置；先验证双端改造与身份绑定，隔离原生兼容路径。"],
["E2EE / 更新供应链","账号服务器被攻陷不能等于可伪装设备；设备 pinning、独立签名与轮换必须在首版包含。"],
["系统权限与虚拟屏","Wayland/Android/Apple 不能无授权万能被控；驱动签名与恢复路径是交付前置。"],
["公网 VR","带宽、抖动、端口、加密与硬件均影响舒适性；LAN/WAN 独立测，不承诺互联网等同本地。"],
["分发 / 授权","Apple 资质与审核、驱动证书、AGPL 与应用商店分发条件需评审；不代用户购买、公开或接受许可变更。"]
]} />
<Table columns={["待审阅项","推荐默认 D（尚未批准）"]} rows={[
["底座与许可证","RustDesk OSS fork＋独立 Go 控制面＋ALVR 适配；遵守实际衍生边界；不依赖 Pro。"],
["网络与安全","13111 同时承载客户端 API；13110 做真实协议改造；成熟 E2EE＋设备指纹固定；自签需线下核验。"],
["账号与权限","初始单用户使用但支持多账号；管理员不默认可控；新设备人工信任；无人值守默认关闭。"],
["分屏语义","L1/L2/L3 都保留；按 OS 分别列能力，不把分区裁剪当虚拟显示器。"],
["VR 起始组合","支持 ALVR 的 Win10/11 PC＋用户实际 Pico 型号先 LAN，再公网；Linux 单独回归。"],
["生成软件与下载","先给预配配置包与可信官方构建；原生生成必须真实构建/签名；下载默认需授权。"],
["数据与更新","最小审计 30 天、签名更新、无静默权限升级；备份与清理由明确管理操作执行。"],
["发布决定","保持设计待审阅；设备清单、架构/OS 下限、签名资质及是否发布商店在开工前确认，不在本轮收集凭据。"]
]} />
<Text>不要求现在逐题回答：这些默认值构成一份可整体审阅的建议。用户可直接指出要改的部分；批准后更新同一 Page 到 v0.2，并将实际设备清单与无法满足的系统限制显式记录，再决定开发范围和阶段出口。全量目标保持，不以默认值替用户接受付费、公开源码方式或删功能决定。</Text>
</Section>

<Section id="s20" heading="20 交给 Cursor 的任务包">
<CodeBlock maxHeight={480}>{`待用户审阅后使用；当前不发送、不启动开发。
项目：CyanDesk
设计基线：本 Page v0.1，须先按用户反馈形成批准版。
现状：仅 seed README；没有已完成远控或部署可复用承诺。

执行约束：
1. 逐项维护 U01—U16 的实现、测试、阻塞与证据，不静默减范围。
2. 先提交技术验证结论和 ADR：RustDesk fork/bridge、Win7 legacy、
   13110 复用与并行竞速、设备 pinning、虚拟屏、ALVR/Apple 分发。
3. 不使用 Pro 私有代码/API；锁定上游 commit、依赖、许可证与 SBOM。
4. 无假登录、假远控、模拟输入或通用下载链接冒充生成软件。
5. 每个里程碑有可复现安装和真机/网络记录；不能仅报告编译通过。
6. 不索要明文凭据、不提交 secret；签名使用隔离受控步骤。
7. 不替用户公开仓库、购买证书、发布商店、联系他人或部署公网。

工作包与出口：
WP0 文档/ADR/威胁模型/平台矩阵/API与协议schema → 评审通过
WP1 上游可行性探针与能力协商 → 真设备证据，阻塞透明
WP2 账号、设备信任、ACL、审计 → 越权/撤销/重放测试通过
WP3 Docker/TLS/管理后台 → 空机安装与恢复可复现
WP4 引擎桥接与真实远控 → 视频、音频、输入、观看隔离
WP5 13110复用/候选竞速/迁移 → 抓包、输入去重与限额证据
WP6 高画质/多屏/L1/L2/L3 → 按平台显示与恢复测试
WP7 OS服务/CLI/移动/Apple/legacy → 全矩阵逐项回归
WP8 ALVR独立适配 → 头追/手柄/音频，LAN/WAN分别验收
WP9 构建/签名/更新/下载 → 制品manifest、篡改拒绝、回滚演练
WP10 全量发布候选 → 安装包、源码/许可、测试报告、剩余风险

每次回报：实际完成项、未实现项、设备/版本、测试证据、
阻塞与需用户决策事项；给出与12月1日完整目标的真实差距。`}</CodeBlock>
<Checklist items={[
{text:"设计原稿包含全量 U01—U16、模块/API/数据、平台/VR/安全/运维与验收",done:true},
{text:"官方上游信息与 CyanDesk 建议设计分开，未验证项明确标 V",done:true},
{text:"用户已审阅并批准本设计",done:false},
{text:"已获批准后交给 Cursor 开发",done:false},
{text:"已实现、构建、签名、部署或验收 CyanDesk",done:false}
]} />
<Text>以上为截至 2026-11-23（北京时间）的记录，不是可由阅读者勾选的待办应用。当前仅交付本设计 Page；下一步是审阅与修订，不是自动开始实现。</Text>
</Section>

<Section id="s21" heading="21 官方资料与证据边界">
<Text>本轮核实日期：2026-11-23（北京时间）。来源用于确定上游能力与限制，不构成 CyanDesk 实现证据；动态主分支不是选定发行版本。开工后必须锁定具体 commit/tag，再逐项源码验证和设备测试。ALVR 主仓库网页摘要信息不足，许可证与平台支持使用官方 raw README/LICENSE 交叉核实；跨网络 Wiki 本轮未取得有效端口正文，保留为待验证而不编造结论。</Text>
<LinkCards numbered links={[
{title:"RustDesk Server OSS：hbbs / hbbr 与开源范围",site:"RustDesk 官方文档",href:"https://rustdesk.com/docs/en/self-host/rustdesk-server-oss/"},
{title:"RustDesk Pro：账号、Web console、API、custom client generator 边界",site:"RustDesk 官方文档",href:"https://rustdesk.com/docs/en/self-host/rustdesk-server-pro/"},
{title:"RustDesk OSS 安装与网络端口",site:"RustDesk 官方文档",href:"https://rustdesk.com/docs/en/self-host/rustdesk-server-oss/install/"},
{title:"Linux：Wayland 实验支持与登录界面限制",site:"RustDesk 官方文档",href:"https://rustdesk.com/docs/en/client/linux/"},
{title:"Android：权限、录屏、输入与 iOS 屏幕共享限制",site:"RustDesk 官方文档",href:"https://rustdesk.com/docs/en/client/android/"},
{title:"RustDesk 客户端源码",site:"GitHub · rustdesk",href:"https://github.com/rustdesk/rustdesk"},
{title:"RustDesk 客户端 AGPL-3.0 原文",site:"官方仓库",href:"https://raw.githubusercontent.com/rustdesk/rustdesk/master/LICENCE"},
{title:"RustDesk OSS 服务端源码",site:"GitHub · rustdesk",href:"https://github.com/rustdesk/rustdesk-server"},
{title:"RustDesk Server AGPL-3.0 原文",site:"官方仓库",href:"https://raw.githubusercontent.com/rustdesk/rustdesk-server/master/LICENSE"},
{title:"ALVR：官方平台、头显、硬件与网络兼容表",site:"官方 README",href:"https://raw.githubusercontent.com/alvr-org/ALVR/master/README.md"},
{title:"ALVR MIT License",site:"官方仓库",href:"https://raw.githubusercontent.com/alvr-org/ALVR/master/LICENSE"},
{title:"ALVR：SteamVR driver、安装与头显配对",site:"官方 Wiki",href:"https://github.com/alvr-org/ALVR/wiki/Installation-guide"},
{title:"ALVR 项目与后续网络/构建文档入口",site:"GitHub · alvr-org",href:"https://github.com/alvr-org/ALVR"}
]} />
<Text>本稿唯一维护位置为此 Page；不另存一份相同正文文件。后续设计反馈、版本与验收状态在此迭代，防止多个“最终稿”产生冲突。</Text>
</Section>
</Page>;
}
