// i18n
const language = document.getElementById("language")
var i18nurl = + language + '.json'

async function getI18n(url) {
  try {
    const response = await fetch(url);
    const data = await response.json();
    return data;
  } catch (error) {
    console.error("读取i18n失败: ", error);
  }
}

i18ndata = getI18n(i18nurl)

// crop image
const title = document.getElementsByTagName("title")
const cropTarget = document.getElementById("cropTarget")
const cropDiv = document.getElementById("cropDiv")
const click1 = document.getElementById("click1")
const click2 = document.getElementById("click2")
const border = document.getElementById("border")
const targetItem = document.getElementById("targetItem")
const WH = [1280, 720]
var clickTimes = 0
var x1, x2, y1, y2

document.getElementById('loader').addEventListener('change', function (e) {
  const reader = new FileReader();
  reader.onload = function (event) {
    cropTarget.src = event.target.result;
  }
  reader.readAsDataURL(e.target.files[0]);
});

cropDiv.addEventListener('click', function (e) {
  clickTimes += 1
  var thisClick = click2
  if (clickTimes % 2 == 1) {
    thisClick = click1
    x1 = e.pageX
    y1 = e.pageY
  }
  else {
    thisClick = click2
    x2 = e.pageX
    y2 = e.pageY
  }

  var offset = 2.5
  thisClick.style.left = e.pageX - offset + "px"
  thisClick.style.top = e.pageY - offset + "px"
  thisClick.className = ""

  if (clickTimes >= 2) {
    const finalX = Math.min(x1, x2);
    const finalY = Math.min(y1, y2);
    const finalW = Math.abs(x2 - x1);
    const finalH = Math.abs(y2 - y1);
    border.style.left = finalX + "px";
    border.style.top = finalY + "px";
    border.style.width = finalW + "px";
    border.style.height = finalH + "px";
    border.className = ""

    const cropTargetRect = cropTarget.getBoundingClientRect()
    const { left: Tl, right: Tr, top: Tt, bottom: Tb } = cropTargetRect
    const Tw = Math.abs(Tr - Tl)
    const Th = Math.abs(Tt - Tb)
    const x1p = Math.abs(x1 - Tl) / Tw
    const x2p = Math.abs(x2 - Tl) / Tw
    const y1p = Math.abs(y1 - Tt) / Th
    const y2p = Math.abs(y2 - Tt) / Th
    const Cw = Math.abs(x1p - x2p) * WH[0]
    const Ch = Math.abs(y1p - y2p) * WH[1]
    const Sx = Math.min(x1p, x2p) * WH[0]
    const Sy = Math.min(y1p, y2p) * WH[1]

    if (Cw > 0 && Ch > 0) {
      const canvas = document.createElement('canvas')
      canvas.width = Cw
      canvas.height = Ch
      const ctx = canvas.getContext('2d')
      ctx.drawImage(cropTarget, Sx, Sy, Cw, Ch, 0, 0, Cw, Ch)
      targetItem.src = canvas.toDataURL("image/png")
    } else {
      targetItem.src = "png/自定义.png"
    }
  }
})

// screenshot
const Bscreenshot = document.getElementById("screenshot")
Bscreenshot.addEventListener('click', async function (e) {
  try {
    const res = await fetch("/screenshot")
    const blob = await res.blob()
    if (!blob.type.startsWith('image/')) {
      console.error("/screenshot:非图片类型", blob.type)
      throw true
    } if (blob.size === 0) {
      throw true
    }
    const imageUrl = URL.createObjectURL(blob)
    cropTarget.src = imageUrl
    alert("成功获取游戏窗口截图")
  } catch (error) {
    console.error(error)
    alert("获取游戏窗口截图失败")
  }
})

// initclick
const initclick = document.getElementById("initclick")
initclick.addEventListener("click", function (e) {
  clickTimes = 0
  click1.className = "hide"
  click2.className = "hide"
  border.className = "hide"
  targetItem.src = "png/自定义.png"
})

// confirm
const Bconfirm = document.getElementById("confirm")
Bconfirm.addEventListener('click', async function (e) {
  try {
    const response = await fetch(targetItem.src)
    const blob = await response.blob()
    const formData = new FormData()
    formData.append("image", blob, "自定义.png")
    const res = await fetch("/save", {
      method: "POST",
      body: formData
    })
    if (res.ok) {
      alert("已保存自定义物品截图，继续搬运流程请点击'退出至流程'")
    } else alert("自定义物品截图保存失败")
  } catch (error) {
    console.error(error)
    alert("自定义物品截图保存失败")
  }
})

// quit
const Bquit = document.getElementById("quit")
Bquit.addEventListener('click', async function (e) {
  try {
    const res = await fetch("/close")
    if (res.ok) {
      alert("退出成功，请关闭本界面")
      closeWindow()
    } else alert("退出失败")
  } catch (error) {
    console.error(error)
    alert("退出失败")
  }
})
