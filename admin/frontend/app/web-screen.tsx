"use client";

import {
  AndroidKeyCode,
  AndroidKeyEventAction,
  AndroidMotionEventAction,
  AndroidMotionEventButton,
  ScrcpyControlMessageSerializer,
  ScrcpyOptions3_3_1,
  ScrcpyPointerId,
} from "@yume-chan/scrcpy";
import {
  WebCodecsVideoDecoder,
  WebGLVideoFrameRenderer,
} from "@yume-chan/scrcpy-decoder-webcodecs";
import { TinyH264Decoder } from "@yume-chan/scrcpy-decoder-tinyh264";
import {
  PushReadableStream,
  type PushReadableStreamController,
} from "@yume-chan/stream-extra";
import {
  KeyboardEvent,
  PointerEvent,
  WheelEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

type ScreenState = "starting" | "connected" | "closed" | "error";

const options = new ScrcpyOptions3_3_1({
  video: true,
  audio: false,
  control: true,
  maxSize: 1280,
  maxFps: 30,
  videoBitRate: 3_000_000,
  tunnelForward: true,
  powerOn: false,
  sendDummyByte: true,
  stayAwake: false,
  powerOffOnClose: false,
  clipboardAutosync: false,
  cleanup: false,
  logLevel: "info",
});

export function BrowserScreenControl() {
  const panelRef = useRef<HTMLElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const serializerRef = useRef(new ScrcpyControlMessageSerializer(options));
  const videoSizeRef = useRef({ width: 1440, height: 3120 });
  const activePointers = useRef(new Set<number>());
  const [state, setState] = useState<ScreenState>("starting");
  const [detail, setDetail] = useState("正在启动手机端视频编码器");
  const [enabled, setEnabled] = useState(true);
  const [generation, setGeneration] = useState(0);
  const [fullscreen, setFullscreen] = useState(false);
  const [powerPending, setPowerPending] = useState<"unlock" | "lock" | null>(null);

  useEffect(() => {
    document.body.classList.add("screen-control-active");
    const onFullscreenChange = () => {
      setFullscreen(document.fullscreenElement === panelRef.current);
    };
    document.addEventListener("fullscreenchange", onFullscreenChange);
    return () => {
      document.body.classList.remove("screen-control-active");
      document.removeEventListener("fullscreenchange", onFullscreenChange);
    };
  }, []);

  const sendControl = useCallback((payload: Uint8Array | undefined) => {
    const socket = socketRef.current;
    if (!payload || !socket || socket.readyState !== WebSocket.OPEN) return;
    const framed = new Uint8Array(payload.byteLength + 1);
    framed[0] = 1;
    framed.set(payload, 1);
    socket.send(framed);
  }, []);

  useEffect(() => {
    if (!enabled || !canvasRef.current) return;
    let disposed = false;
    let videoController: PushReadableStreamController<Uint8Array> | undefined;
    let disposeDecoder: (() => void) | undefined;
    let removeSizeListener: (() => void) | undefined;
    let frameWatchdog: number | undefined;
    let enqueueChain = Promise.resolve();
    const pointers = activePointers.current;
    const video = new PushReadableStream<Uint8Array>((controller) => {
      videoController = controller;
    });
    const scheme = location.protocol === "https:" ? "wss" : "ws";
    const socket = new WebSocket(`${scheme}://${location.host}/api/screen`);
    socket.binaryType = "arraybuffer";
    socketRef.current = socket;
    setState("starting");
    setDetail("正在启动手机端视频编码器");

    const decode = async () => {
      const stream = await options.parseVideoStreamMetadata(video);
      if (disposed || !canvasRef.current) return;
      if (stream.metadata.width && stream.metadata.height) {
        videoSizeRef.current = {
          width: stream.metadata.width,
          height: stream.metadata.height,
        };
      }
      const packets = stream.stream.pipeThrough(
        options.createMediaStreamTransformer(),
      );
      const canvas = canvasRef.current;
      const watchFirstFrame = (
        decoder: { framesRendered: number },
        decoderLabel: string,
      ) => {
        const deadline = Date.now() + 12_000;
        setDetail(`${decoderLabel} · 等待第一帧`);
        frameWatchdog = window.setInterval(() => {
          if (disposed) return;
          if (decoder.framesRendered > 0) {
            if (frameWatchdog !== undefined) window.clearInterval(frameWatchdog);
            frameWatchdog = undefined;
            setDetail(`${decoderLabel} · 1280p 上限 · 30 FPS`);
            setState("connected");
          } else if (Date.now() >= deadline) {
            if (frameWatchdog !== undefined) window.clearInterval(frameWatchdog);
            frameWatchdog = undefined;
            setState("error");
            setDetail("视频通道已连接，但解码器在 12 秒内没有输出画面");
            socket.close();
          }
        }, 250);
      };
      if (
        WebCodecsVideoDecoder.isSupported &&
        WebGLVideoFrameRenderer.isSupported
      ) {
        const renderer = new WebGLVideoFrameRenderer(canvas);
        const decoder = new WebCodecsVideoDecoder({
          codec: stream.metadata.codec,
          renderer,
        });
        removeSizeListener = decoder.sizeChanged((size) => {
          videoSizeRef.current = size;
        });
        disposeDecoder = () => decoder.dispose();
        watchFirstFrame(decoder, "硬件解码");
        await packets.pipeTo(decoder.writable);
      } else {
        const decoder = new TinyH264Decoder({ canvas });
        removeSizeListener = decoder.sizeChanged((size) => {
          videoSizeRef.current = size;
        });
        disposeDecoder = () => decoder.dispose();
        watchFirstFrame(decoder, "软件解码");
        await packets.pipeTo(decoder.writable);
      }
    };

    decode().catch((error) => {
      if (disposed) return;
      setState("error");
      setDetail(error instanceof Error ? error.message : "视频解码失败");
      socket.close();
    });

    socket.onmessage = (event) => {
      if (typeof event.data === "string") {
        try {
          const message = JSON.parse(event.data) as {
            type?: string;
            message?: string;
          };
          if (message.type === "error") {
            setState("error");
            setDetail(message.message || "手机端屏幕服务启动失败");
          }
        } catch {
          // Ignore unknown status messages; video always arrives as binary.
        }
        return;
      }
      if (!(event.data instanceof ArrayBuffer)) return;
      const payload = new Uint8Array(event.data);
      if (payload.length < 2 || payload[0] !== 0 || !videoController) return;
      const chunk = payload.slice(1);
      enqueueChain = enqueueChain
        .then(() => videoController?.enqueue(chunk))
        .then(() => undefined)
        .catch(() => undefined);
    };
    socket.onerror = () => {
      if (!disposed) {
        setState("error");
        setDetail("无法建立屏幕控制通道");
      }
    };
    socket.onclose = () => {
      enqueueChain.finally(() => {
        try {
          videoController?.close();
        } catch {
          // Decoder may already have closed the stream.
        }
      });
      if (!disposed) {
        setState((current) => (current === "error" ? current : "closed"));
        setDetail((current) =>
          current.includes("失败") ? current : "屏幕会话已结束",
        );
      }
    };

    return () => {
      disposed = true;
      pointers.clear();
      socketRef.current = null;
      socket.close(1000, "page closed");
      enqueueChain.finally(() => {
        try {
          videoController?.close();
        } catch {
          // Stream already closed.
        }
      });
      removeSizeListener?.();
      if (frameWatchdog !== undefined) window.clearInterval(frameWatchdog);
      disposeDecoder?.();
    };
  }, [enabled, generation]);

  const point = (event: PointerEvent<HTMLCanvasElement>) => {
    const rectangle = event.currentTarget.getBoundingClientRect();
    const size = videoSizeRef.current;
    return {
      x: Math.max(
        0,
        Math.min(
          size.width - 1,
          Math.round(((event.clientX - rectangle.left) / rectangle.width) * size.width),
        ),
      ),
      y: Math.max(
        0,
        Math.min(
          size.height - 1,
          Math.round(((event.clientY - rectangle.top) / rectangle.height) * size.height),
        ),
      ),
    };
  };

  const touch = (
    event: PointerEvent<HTMLCanvasElement>,
    action: (typeof AndroidMotionEventAction)[keyof typeof AndroidMotionEventAction],
  ) => {
    event.preventDefault();
    const socket = socketRef.current;
    if (
      action === AndroidMotionEventAction.Move &&
      socket &&
      socket.bufferedAmount > 512 * 1024
    ) {
      return;
    }
    const position = point(event);
    const size = videoSizeRef.current;
    const released =
      action === AndroidMotionEventAction.Up ||
      action === AndroidMotionEventAction.Cancel;
    sendControl(
      serializerRef.current.injectTouch({
        action,
        pointerId:
          event.pointerType === "mouse"
            ? ScrcpyPointerId.Mouse
            : BigInt(event.pointerId),
        pointerX: position.x,
        pointerY: position.y,
        videoWidth: size.width,
        videoHeight: size.height,
        pressure: released ? 0 : event.pressure || 1,
        actionButton: AndroidMotionEventButton.None,
        buttons: released
          ? AndroidMotionEventButton.None
          : AndroidMotionEventButton.Primary,
      }),
    );
  };

  const pressKey = useCallback(
    (keyCode: (typeof AndroidKeyCode)[keyof typeof AndroidKeyCode]) => {
      const serializer = serializerRef.current;
      sendControl(
        serializer.injectKeyCode({
          action: AndroidKeyEventAction.Down,
          keyCode,
          repeat: 0,
          metaState: 0,
        }),
      );
      sendControl(
        serializer.injectKeyCode({
          action: AndroidKeyEventAction.Up,
          keyCode,
          repeat: 0,
          metaState: 0,
        }),
      );
    },
    [sendControl],
  );

  const keyboard = (event: KeyboardEvent<HTMLDivElement>) => {
    const keyCodes: Record<string, number> = {
      Enter: AndroidKeyCode.Enter,
      Backspace: AndroidKeyCode.Backspace,
      Delete: AndroidKeyCode.Delete,
      Escape: AndroidKeyCode.AndroidBack,
      Tab: AndroidKeyCode.Tab,
      ArrowUp: AndroidKeyCode.ArrowUp,
      ArrowDown: AndroidKeyCode.ArrowDown,
      ArrowLeft: AndroidKeyCode.ArrowLeft,
      ArrowRight: AndroidKeyCode.ArrowRight,
    };
    if (keyCodes[event.key] !== undefined) {
      event.preventDefault();
      pressKey(keyCodes[event.key] as never);
    } else if (event.key.length === 1 && !event.ctrlKey && !event.metaKey) {
      event.preventDefault();
      sendControl(serializerRef.current.injectText(event.key));
    }
  };

  const wheel = (event: WheelEvent<HTMLCanvasElement>) => {
    event.preventDefault();
    const rectangle = event.currentTarget.getBoundingClientRect();
    const size = videoSizeRef.current;
    const payload = serializerRef.current.injectScroll({
      pointerX: Math.round(
        ((event.clientX - rectangle.left) / rectangle.width) * size.width,
      ),
      pointerY: Math.round(
        ((event.clientY - rectangle.top) / rectangle.height) * size.height,
      ),
      videoWidth: size.width,
      videoHeight: size.height,
      scrollX: -Math.sign(event.deltaX),
      scrollY: -Math.sign(event.deltaY),
      buttons: AndroidMotionEventButton.None,
    });
    sendControl(payload);
  };

  const reconnect = () => {
    setEnabled(true);
    setGeneration((value) => value + 1);
  };

  const devicePower = async (action: "unlock" | "lock") => {
    if (powerPending) return;
    setPowerPending(action);
    try {
      const response = await fetch("/api/device/power", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ action }),
      });
      const result = (await response.json().catch(() => ({}))) as { error?: string };
      if (!response.ok) throw new Error(result.error || "手机屏幕操作失败");
      setDetail(
        action === "unlock"
          ? "手机已唤醒并解除滑动锁屏"
          : "手机已锁屏；路由服务继续运行",
      );
    } catch (error) {
      setDetail(error instanceof Error ? error.message : "手机屏幕操作失败");
    } finally {
      setPowerPending(null);
    }
  };

  const toggleFullscreen = async () => {
    if (document.fullscreenElement) {
      await document.exitFullscreen();
    } else {
      await panelRef.current?.requestFullscreen();
    }
  };

  return (
    <article className="panel screen-panel" ref={panelRef}>
      <header className="screen-toolbar">
        <div>
          <span className={`terminal-state ${state}`} />
          <strong>
            {state === "connected"
              ? "屏幕已连接"
              : state === "starting"
                ? "正在启动"
                : state === "error"
                  ? "连接失败"
                  : "连接已关闭"}
          </strong>
          <small>{detail}</small>
        </div>
        <div className="screen-actions">
          <button
            type="button"
            disabled={powerPending !== null}
            onClick={() => devicePower("unlock")}
          >
            {powerPending === "unlock" ? "正在唤醒" : "唤醒并解锁"}
          </button>
          <button
            type="button"
            disabled={powerPending !== null}
            onClick={() => devicePower("lock")}
          >
            {powerPending === "lock" ? "正在锁屏" : "锁屏"}
          </button>
          <button type="button" onClick={toggleFullscreen}>
            {fullscreen ? "退出全屏" : "全屏显示"}
          </button>
          <button type="button" onClick={() => pressKey(AndroidKeyCode.AndroidBack)}>
            返回
          </button>
          <button type="button" onClick={() => pressKey(AndroidKeyCode.AndroidHome)}>
            主页
          </button>
          <button type="button" onClick={() => pressKey(AndroidKeyCode.AndroidAppSwitch)}>
            最近任务
          </button>
          {enabled && (state === "connected" || state === "starting") ? (
            <button type="button" onClick={() => setEnabled(false)}>
              结束
            </button>
          ) : (
            <button type="button" onClick={reconnect}>
              重新连接
            </button>
          )}
        </div>
      </header>
      <div
        className="screen-stage"
        tabIndex={0}
        onKeyDown={keyboard}
        aria-label="一加 7 Pro 屏幕控制"
      >
        <canvas
          ref={canvasRef}
          onPointerDown={(event) => {
            activePointers.current.add(event.pointerId);
            event.currentTarget.setPointerCapture(event.pointerId);
            touch(event, AndroidMotionEventAction.Down);
            event.currentTarget.parentElement?.focus();
          }}
          onPointerMove={(event) => {
            if (activePointers.current.has(event.pointerId)) {
              touch(event, AndroidMotionEventAction.Move);
            }
          }}
          onPointerUp={(event) => {
            touch(event, AndroidMotionEventAction.Up);
            activePointers.current.delete(event.pointerId);
          }}
          onPointerCancel={(event) => {
            touch(event, AndroidMotionEventAction.Cancel);
            activePointers.current.delete(event.pointerId);
          }}
          onWheel={wheel}
        />
        {state !== "connected" && (
          <div className="screen-overlay">
            <span className="phone-ring">1+</span>
            <strong>{detail}</strong>
          </div>
        )}
      </div>
      <footer className="terminal-help">
        点击或触摸画面可直接操作手机；画面区域获得焦点后支持键盘输入。离开本页会自动停止编码。
      </footer>
    </article>
  );
}
