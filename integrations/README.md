# integrations/

隔离区：放置独立于上游 gptgrok2api 的第三方或自研项目。

## 约定

- 本目录下的每个子项目都是**独立 Git 仓库**，以 submodule 形式引入
- 上游 `git pull` 不会进入本目录，因此**更新上游不会影响这里的项目**
- 子项目自行维护版本与发布节奏，通过 HTTP / 环境变量与主服务交互

## 接入方式

```bash
# 1. 把项目放到本目录
git mv /path/to/project integrations/project

# 2. 若还不是独立仓库，先初始化
cd integrations/project && git init && git add . && git commit -m "init"
git remote add origin <repo url>

# 3. 在主仓库注册为 submodule
cd <主仓库根目录>
git submodule add <repo url> integrations/project
```

## 更新

```bash
# 更新子项目
git submodule update --remote integrations/project
git add integrations/project && git commit -m "chore: bump project"

# 更新上游（不触碰本目录）
git pull origin main
```

## 克隆主仓库

```bash
git clone --recurse-submodules <主仓库 url>
# 已克隆但未初始化子模块时
git submodule update --init --recursive
```
