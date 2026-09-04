# Frontend Development Spec 文档规范

Frontend Engineer 使用本模板说明当前项目的前端如何组织和开发。文档不是 Frontend 教程，也不描述 Workflow 协作职责。

## 必须说明

- Frontend 使用的技术栈和版本约束；
- 前端工程的目录结构；
- 页面、组件、状态、数据和资源目录的职责；
- 路由、页面入口和主要交互流程；
- API 依赖和数据流；
- 加载、错误、空状态和权限状态；
- Design Markdown 的位置和使用方式；
- 项目特有的开发约束和实现边界。

工程目录必须使用文件树展示，并在节点旁说明目录或关键文件的职责，例如：`src/components/`（页面组件）。

Design Markdown 必须描述整个项目的设计系统和审美方向，并说明前端如何遵循它。

## 合格标准

文档能够让新的 Frontend Engineer 明确本项目使用什么技术栈、页面、组件和状态应该放在哪里，以及如何遵循项目 Design Markdown。

技术栈没有被 Product Concept 或项目已有约束指定时，选择简单、成熟、适合当前目标的方案。产品形态或技术栈未确认时，不进入工程初始化。
