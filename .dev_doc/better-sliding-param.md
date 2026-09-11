# BetterSliding 参数更新

## 新增参数

新增参数A和B，参数名请根据实际情况自行推断，以下是参数说明

### Param A

类型：Bool / Int

用于控制是否微调（Increase）

#### 取值true

BetterSlidingCheckQuantity后通过Increase/Decrease进行微调

#### 取值false

BetterSlidingCheckQuantity后不通过Increase/Decrease进行微调

#### 取值类型为int

根据BetterSlidingCheckQuantity结果判断是否进行微调，小小于等于设置值则进行微调

### Param B

类型：String（none/more/less）

用于控制不进行微调时的行为

#### 取值none

不进行任何操作，直接返回成功

即，当A=false，B=none时，与现有参数`FinishAfterPerciseclick`行为一致

#### 取值more

在BetterSlidingCheckQuantity判断不进行微调后，确认当前SliderQuantity是否大于Target。如果小于Target，则将PerciseClick的坐标往正方向移动1px

1px仅在x或y单方向上进行操作，具体取决于Start与End的坐标差值，若x差值大于y差值，则在x方向上移动1px，否则在y方向上移动1px

#### 取值less

与取值more类似，只是将PerciseClick的坐标往负方向移动1px

## 移除参数

移除`FinishAfterPerciseclick`参数，改为使用新增参数A和B进行控制

## pipeline 流程

本次在pipeline中的更改已有由户手动进行操作，即：`BetterSlidingCheckQuantity`后新接`BetterSlidingPreciseClick`，具体next override仍由go侧负责，pipeline仅作为大致示意
