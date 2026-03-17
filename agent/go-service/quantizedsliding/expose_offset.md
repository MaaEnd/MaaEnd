# 暴露 quantizedsliding 中 centerPointOffset 给 custom action param

## 说明

将常量 centerPointOffset 更改为暴露的变量，以便在 custom action 中使用。

参数格式为: [x, y]，表示相对于滑动按钮中心点的偏移量。（实际效果为微调PreciseClick的点击位置）

如果未传入，则默认使用 [-10, 0]，表示向左偏移 10 个像素。

**如有必要，可更改centerPointOffset名称，使其更好地描述其功能**

```json
"SomeTaskAdjustQuantity": {
    "action": {
        "type": "Custom",
        "param": {
            "custom_action": "QuantizedSliding",
            "custom_action_param": {
                "Target": 1,
                "QuantityBox": [360, 490, 110, 70],
                "Direction": "right",
                "IncreaseButton": "AutoStockpile/IncreaseButton.png",
                "DecreaseButton": "AutoStockpile/DecreaseButton.png",
                "centerPointOffset": [-10, 0]
            }
        }
    }
}
```

## 注意事项

在完成更改后，请同步信息到开发文档中

- [QuantizedSliding(zh_cn)](../../../docs/zh_cn/developers/quantized-sliding.md)
- [QuantizedSliding(en_us)](../../../docs/en_us/developers/quantized-sliding.md)
