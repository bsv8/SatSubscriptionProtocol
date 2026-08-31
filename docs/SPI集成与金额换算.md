# SSP 与 SPI 集成及金额换算

本文冻结使用 SSP + SPI 的应用层金额规则，不修改 SSP Wire。SSP 核心 SDK 不依赖 SPI，
也不读取价格配置；`Backend`/`Dispatcher` 由服务程序实现实际收费。

SPI V1 的 `balance` 和 `amount` 是对应资产最小单位的 `uint64` 整数（见
[SPI CDDL](../../SPI/spec/v1/spi.cddl)）。SSP 的 `charged_amount` 是同一最小单位的
规范十进制字符串，但允许小于一个 SPI 最小单位的小数。因此不能把 SSP 金额直接转换
成 JavaScript `number`、Go `float64` 或 SPI `uint64` 后再四舍五入。

## 1. 固定 scale

应用层统一使用固定 `scale = 18`：

```text
1 个 SPI 最小单位 = 10^18 个 SSP 账本子单位
```

`scale` 是部署级常量，不能随请求、账户或货币变化。金额字符串去掉小数点后得到整数
`N`，小数位数为 `f` 时，账本整数为：

```text
SSP 子单位金额 = N × 10^(18 - f)
```

示例：

| `charged_amount` | SSP 账本子单位 |
|---:|---:|
| `0` | `0` |
| `0.001` | `1000000000000000` |
| `1.2` | `1200000000000000000` |
| `12.34` | `12340000000000000000` |
| `0.123456789012345678` | `123456789012345678` |

Go 应使用 `math/big.Int` 或等价的无溢出整数实现；TypeScript 应使用 `bigint`。SPI
`uint64` 的最大余额乘以 `10^18` 可能超出 `uint64`，必须在转换前后执行范围检查。

## 2. 余额、扣费和结算

SPI 余额 `B_spi` 转为 SSP 账本初始余额时使用：

```text
B_ssp = B_spi × 10^18
```

每个 SSP 动作先把 `charged_amount` 精确转换为子单位整数，随后在同一账户事务中完成：

1. 锁定账户行或使用等价串行化机制；
2. 检查 `B_ssp >= fee_ssp`；
3. 扣除 `fee_ssp`，并更新订阅状态或 Publish 接受状态；
4. 将原始精确金额字符串作为 `charged_amount` 返回。

如果应用需要把累计费用结算到 SPI，只能结算完整的 SPI 最小单位：

```text
collect_units = pending_ssp / 10^18
remainder_ssp = pending_ssp % 10^18
```

余数必须继续保存在应用层整数账本中，不能向上、向下或四舍五入后伪造本次 SSP 扣费。
没有固定 scale 账本的应用不能支持小数价格，应在价格配置或事务开始前拒绝，而不是
静默截断金额。

## 3. 展示规则

- Wire 中始终展示规范 `charged_amount` 字符串，例如 `0.001`；不改成浮点数。
- 应用账本展示使用子单位整数按固定 18 位小数格式化；格式化过程仍使用字符串。
- SPI 钱包余额展示使用货币配置中的显示小数位，不把 SPI `uint64` 转成 JavaScript
  `number`。显示小数位属于应用配置，不进入 SSP Wire。
- SSP 金额的 18 位 scale 是“SPI 最小单位的细分账本”，不是货币名称或货币显示精度。

## 4. 事务和失败边界

SSP 的 Backend/Dispatcher 必须把收费与业务动作放在同一原子边界：

- Subscribe/Unsubscribe 的检查、子单位扣费和状态写入必须一起提交；
- SubscriptionsRequest 的列表读取和扣费必须来自同一事务快照；
- Publish 的扣费和“接受并开始广播”必须一起提交；
- 任一检查、余额或业务步骤失败时，不扣费、不留下部分状态；
- SPI Collect 与 SSP 扣费并发触碰同一账户时，使用同一账户行锁或等价串行化。

这些规则由使用 SDK 的应用实现。SSP SDK 只校验 `charged_amount` 的规范字符串和
Backend/Dispatcher 返回的订阅数据，不实现 SPI 余额、换算或数据库事务。
