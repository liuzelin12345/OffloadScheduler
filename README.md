# OffloadScheduler（基于 Kubernetes v1.33.0）

本项目基于 Kubernetes `v1.33.0` 二次开发，新增了一个自定义调度插件 `OffloadScheduler`，并将编译成果输出目录 `_output` 一并纳入仓库，便于复现实验与快速验证。

## 项目改动概览

相对于原生 Kubernetes 源码，主要改动如下：

1. 新增自定义调度插件：
   - 目录：`pkg/scheduler/framework/plugins/customscheduler`
   - 文件：`pkg/scheduler/framework/plugins/customscheduler/offloadscheduler.go`
   - 插件名：`OffloadScheduler`

2. 将插件注册到调度器插件注册表：
   - 文件：`pkg/scheduler/framework/plugins/registry.go`
   - 注册项：`customscheduler.Name: customscheduler.New`

3. 将编译输出目录加入仓库：
   - 目录：`_output`
   - 作用：保存本地构建过程产生的中间文件与得到的kube-scheduler二进制文件。

4. 修改了一个官方构建脚本不适配较新版本docker的bug：
   - 文件：`build/common.sh`
   - 修改前：container_ip=$("${DOCKER[@]}" inspect --format '{{ .NetworkSettings.IPAddress }}' "${KUBE_RSYNC_CONTAINER_NAME}")
   - 修改后：container_ip=$("${DOCKER[@]}" inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${KUBE_RSYNC_CONTAINER_NAME}")
   - 作用：官方使用交叉编译容器进行源码编译，在获取容器ip时匹配的是旧版本docker的API。在使用较新版本docker时脚本会报错，该改动修复了报错。

## OffloadScheduler 插件说明

`OffloadScheduler` 实现了 `PreScore` 和 `Score` 两个阶段，核心目标是：

- 根据 Pod 资源请求（CPU/内存）与节点可用资源的匹配程度打分；
- 实现机器人节点向边缘服务器节点卸载计算任务的逻辑；
- 避免单纯偏向“剩余资源最大”的节点，从而降低资源碎片化风险；

### 打分逻辑

1. `PreScore` 阶段：
   - 读取待调度 Pod 的 CPU/内存请求，写入 `CycleState`；
   - 若 Pod 为 BestEffort（无显式请求），则跳过后续有意义的资源匹配打分。

2. `Score` 阶段：
   - 读取 `PreScore` 写入的请求值；
   - 分别计算 CPU 与内存的匹配分；
   - 按 Pod 请求量对 CPU/内存分数加权；
   - 输出范围限制在调度器标准分值区间（`0~100`）。

## 如何构建

在项目根目录执行（与 Kubernetes 源码构建方式一致）：

```bash
./build/run.sh make kube-scheduler
```


