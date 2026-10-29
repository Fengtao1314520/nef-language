import { defineConfig } from 'vitepress'
import { highlightNef } from './theme/nef-highlight.mjs'

// 「语法参考」这一栏同时挂在 /reference/ 与根路径下的 /reference/034_syntax 上，两处的条目必须一模一样
const languageSidebar = [
  {
    text: '语法参考',
    items: [
      { text: '语法参考总览', link: '/reference/033_index' },
      { text: '语法速查', link: '/reference/034_syntax' },
      { text: '类型一览', link: '/reference/035_types' },
      { text: '值与存放', link: '/reference/038_memory' },
      { text: '函数与调用', link: '/reference/039_functions' },
      { text: '语句与块', link: '/reference/036_statements' },
      { text: '容器', link: '/reference/037_containers' },
      { text: '定义', link: '/reference/040_definitions' },
      { text: '出错的时候', link: '/reference/041_errors' },
      { text: '多个文件', link: '/reference/042_modules' },
      { text: '术语表', link: '/reference/043_glossary' }
    ]
  }
]

const toolsSidebar = [
  {
    text: '工具与调试',
    items: [
      { text: '命令行工具', link: '/tools/059_cli' },
      { text: '错误信息索引', link: '/tools/060_errors' },
      { text: '编辑器与调试', link: '/tools/061_editors' }
    ]
  }
]

const libsSidebar = [
  {
    text: '库',
    items: [
      { text: '浏览器驱动库 web_test', link: '/libs/062_web_test' },
      { text: '用例运行层 test_run', link: '/libs/063_test_run' }
    ]
  }
]

// base 由构建时的环境变量决定：GitHub Pages 的项目站点要把站点放在仓库名那一段路径下
// （工作流传 BASE_PATH=/仓库名/），换成自己的域名时不设这个变量，就落在根路径。
const base = process.env.BASE_PATH ?? '/'

export default defineConfig({
  base,
  lang: 'zh-CN',
  title: 'Nef',
  titleTemplate: ':title | Nef',
  description: 'Nef 编译为机器码：无运行时依赖、无垃圾回收；类型与所有权在编译期确定。',
  cleanUrls: true,

  // 编号规矩的唯一例外：站点首页的文件名必须留在 `index.md`。全站正文都用
  // `NNN_名字.md` 的号，唯独这一份不行——实测 frontmatter 的 `route: /` 与 config 的
  // `rewrites`（试过 'index'、'index.md'、'' 三种目标）都换不来根路径，dist 里只会出
  // `100_index.html`，`/` 就 404 了。所以首页不编号，其余地址一律跟着文件名走。
  lastUpdated: false,
  // site/README.md 是站点这一格自己的 readme，给在仓库里翻目录的人看，不是网页。
  // 不收掉的话站上会多出一个 /README，与首页重复；而且它引用仓库内的其他文档，
  // 那些路径在 srcDir 之外，会被死链检查判红。
  srcExclude: ['README.md'],

  head: [
    ['link', { rel: 'icon', href: `${base}favicon.svg` }],
    ['meta', { name: 'theme-color', content: '#c6402e' }]
  ],

  themeConfig: {
    siteTitle: 'Nef',
    // 以 `/` 起头，VitePress 会替它补上站点前缀；自己再拼一次 base 会变成两段。
    logo: { light: '/logo.svg', dark: '/logo-dark.svg', alt: 'Nef' },
    nav: [
      { text: '入门指南', link: '/learn/021_index', activeMatch: '/learn/' },
      { text: '语法参考', link: '/reference/033_index', activeMatch: '/reference/' },
      { text: '标准库', link: '/stdlib/044_index', activeMatch: '/stdlib/' },
      { text: '库', link: '/libs/062_web_test', activeMatch: '/libs/' },
      {
        text: '工具与调试',
        items: [
          { text: '命令行工具', link: '/tools/059_cli' },
          { text: '错误信息索引', link: '/tools/060_errors' },
          { text: '编辑器与调试', link: '/tools/061_editors' }
        ]
      },
      { text: '关于', link: '/about/064_design' }
    ],
    sidebar: {
      '/learn/': [
        {
          text: '地基',
          items: [
            { text: '第 0 课：开始之前', link: '/learn/021_index' },
            { text: '第 1 课：应用与入口', link: '/learn/022_start' },
            { text: '第 2 课：变量与类型标注', link: '/learn/023_values' },
            { text: '第 3 课：数字与小数', link: '/learn/024_numbers' },
            { text: '第 4 课：文本与码点', link: '/learn/025_text' }
          ]
        },
        {
          // 这门语言与别的脚本语言分岔的地方，排在控制流与容器之前：
          // 传参、字段、装箱都要用到它，读到那一课才撞见就太晚了
          text: '核心思维',
          items: [
            { text: '第 5 课：值的所有权', link: '/learn/026_ownership' },
            { text: '第 6 课：判断与循环', link: '/learn/027_flow' },
            { text: '第 7 课：容器类型', link: '/learn/028_collections' }
          ]
        },
        {
          text: '自定义类型',
          items: [
            { text: '第 8 课：自定义类型', link: '/learn/029_types' },
            { text: '第 9 课：接口与枚举', link: '/learn/030_contracts' }
          ]
        },
        {
          text: '工程化',
          items: [
            { text: '第 10 课：错误处理', link: '/learn/031_errors' },
            { text: '第 11 课：多文件组织', link: '/learn/032_files' }
          ]
        }
      ],
      '/reference/': languageSidebar,
      '/stdlib/': [
        {
          text: '标准库',
          items: [
            { text: '标准库总览', link: '/stdlib/044_index' },
            { text: '屏幕输出与输入', link: '/stdlib/045_console' },
            { text: '文本处理', link: '/stdlib/046_text' },
            { text: '字节块', link: '/stdlib/047_bytes' },
            { text: '数值与数学', link: '/stdlib/048_number' },
            { text: '列表·字典·集合', link: '/stdlib/049_containers' },
            { text: '读写文件', link: '/stdlib/050_file' },
            { text: '路径', link: '/stdlib/051_path' },
            { text: '环境变量与当前目录', link: '/stdlib/052_environment' },
            { text: '进程', link: '/stdlib/053_process' },
            { text: '时间', link: '/stdlib/054_time' },
            { text: '正则表达式', link: '/stdlib/055_regular_expression' },
            { text: '解析 JSON', link: '/stdlib/056_json' },
            { text: 'TCP 套接字', link: '/stdlib/057_net' },
            { text: 'HTTP 客户端', link: '/stdlib/058_http' }
          ]
        }
      ],
      '/libs/': libsSidebar,
      '/tools/': toolsSidebar,
      '/about/': [
        {
          text: '关于',
          items: [
            { text: '设计取舍', link: '/about/064_design' },
            { text: '许可与商标', link: '/about/065_license' },
            { text: '版本与兼容', link: '/about/066_versions' }
          ]
        }
      ]
    },
    outline: { level: [2, 3], label: '本页小节' },
    docFooter: { prev: '上一页', next: '下一页' },
    search: { provider: 'local' },
    editLink: undefined,
    socialLinks: undefined,
    // 句出苏轼《前赤壁赋》："且夫天地之间，物各有主，苟非吾之所有，虽一毫而莫取。"
    // 后半句说的正是这个语言的那一条：不是自己的名字持有，就交不出值。
    footer: { message: '天地之间，物各有主；苟非吾之所有，虽一毫而莫取。' }
  },

  markdown: {
    lineNumbers: false,
    config(md) {
      const upstream = md.options.highlight
      md.options.highlight = (code, lang, attrs) => {
        // `nef` 用于包与库的示例：这段代码要连同 `libs/` 一起才编译得起来，
        // 检查脚本只跑 `nf`／`nf-bad`／`nf-frag` 三种标记的块，这里照样交给同一个高亮器。
        if (lang === 'nf' || lang === 'nf-frag' || lang === 'nf-bad' || lang === 'nef') return highlightNef(code)
        return upstream ? upstream(code, lang, attrs) : ''
      }
    }
  }
})
