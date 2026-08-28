"use client";

import {
  AndroidKeyCode,
  AndroidKeyEventAction,
  AndroidKeyEventMeta,
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
  BufferedReadableStream,
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
const MAX_CLIPBOARD_BYTES = 240 * 1024;

function createScrcpyOptions() {
  return new ScrcpyOptions3_3_1({
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
    clipboardAutosync: true,
    cleanup: false,
    logLevel: "info",
  });
}

async function writeBrowserClipboard(text: string) {
  if (window.isSecureContext && navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // The click-triggered legacy path also works on the LAN HTTP dashboard.
    }
  }

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  document.body.appendChild(textarea);
  textarea.select();
  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) throw new Error("浏览器拒绝写入剪切板");
}

export function BrowserScreenControl() {
  const panelRef = useRef<HTMLElement>(null);
  const stageRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const socketRef = useRef<WebSocket | null>(null);
  const serializerRef = useRef(
    new ScrcpyControlMessageSerializer(createScrcpyOptions()),
  );
  const clipboardSequenceRef = useRef(BigInt(1));
  const videoSizeRef = useRef({ width: 1440, height: 3120 });
  const activePointers = useRef(new Set<number>());
  const [state, setState] = useState<ScreenState>("starting");
  const [detail, setDetail] = useState("正在启动手机端视频编码器");
  const [enabled, setEnabled] = useState(true);
  const [generation, setGeneration] = useState(0);
  const [fullscreen, setFullscreen] = useState(false);
  const [powerPending, setPowerPending] = useState<"unlock" | "lock" | null>(null);
  const [deviceClipboard, setDeviceClipboard] = useState<string | null>(null);
  const [clipboardStatus, setClipboardStatus] = useState(
    "剪切板已启用 · 在画面中按 Ctrl+V 可粘贴到手机",
  );

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
    if (!payload || !socket || socket.readyState !== WebSocket.OPEN) return false;
    const framed = new Uint8Array(payload.byteLength + 1);
    framed[0] = 1;
    framed.set(payload, 1);
    socket.send(framed);
    return true;
  }, []);

  const sendClipboardToDevice = useCallback(
    async (text: string, paste: boolean) => {
      const byteLength = new TextEncoder().encode(text).byteLength;
      if (byteLength > MAX_CLIPBOARD_BYTES) {
        setClipboardStatus("剪切板内容超过 240 KiB，未发送到手机");
        return false;
      }
      try {
        const sequence = clipboardSequenceRef.current;
        clipboardSequenceRef.current += BigInt(1);
        // OxygenOS may update the clipboard but ignore scrcpy's immediate
        // paste flag. Request an acknowledgement first, then inject Ctrl+V
        // into the focused Android field after the clipboard is committed.
        const serialized = serializerRef.current.setClipboard({
          sequence,
          paste: false,
          content: text,
        });
        let payload: Uint8Array;
        let acknowledgement: Promise<void> | undefined;
        if (Array.isArray(serialized)) {
          [payload, acknowledgement] = serialized;
        } else {
          payload = serialized;
        }
        if (!sendControl(payload)) {
          setClipboardStatus("屏幕尚未连接，剪切板内容未发送");
          return false;
        }

        if (acknowledgement) {
          await Promise.race([
            acknowledgement,
            new Promise<void>((resolve) => window.setTimeout(resolve, 1000)),
          ]);
        }

        if (paste) {
          await new Promise<void>((resolve) => window.setTimeout(resolve, 50));
          const serializer = serializerRef.current;
          const metaState = AndroidKeyEventMeta.Ctrl;
          sendControl(
            serializer.injectKeyCode({
              action: AndroidKeyEventAction.Down,
              keyCode: AndroidKeyCode.KeyV,
              repeat: 0,
              metaState,
            }),
          );
          sendControl(
            serializer.injectKeyCode({
              action: AndroidKeyEventAction.Up,
              keyCode: AndroidKeyCode.KeyV,
              repeat: 0,
              metaState,
            }),
          );
        }

        setClipboardStatus(
          paste
            ? `已发送并粘贴到手机（${text.length} 个字符）`
            : `已同步到手机（${text.length} 个字符）`,
        );
        return true;
      } catch {
        setClipboardStatus("手机剪切板写入或粘贴失败");
        return false;
      }
    },
    [sendControl],
  );

  useEffect(() => {
    const onPaste = (event: globalThis.ClipboardEvent) => {
      const target = event.target;
      if (!panelRef.current || !(target instanceof Node) || !panelRef.current.contains(target)) {
        return;
      }
      const text = event.clipboardData?.getData("text/plain");
      if (text === undefined) return;
      event.preventDefault();
      void sendClipboardToDevice(text, true);
    };
    document.addEventListener("paste", onPaste);
    return () => document.removeEventListener("paste", onPaste);
  }, [sendClipboardToDevice]);

  useEffect(() => {
    if (!enabled || !canvasRef.current) return;
    let disposed = false;
    let videoController: PushReadableStreamController<Uint8Array> | undefined;
    let deviceMessageController:
      | PushReadableStreamController<Uint8Array>
      | undefined;
    let disposeDecoder: (() => void) | undefined;
    let removeSizeListener: (() => void) | undefined;
    let frameWatchdog: number | undefined;
    let deviceMessagesEnded = false;
    let enqueueChain = Promise.resolve();
    let deviceMessageChain = Promise.resolve();
    const pointers = activePointers.current;
    const sessionOptions = createScrcpyOptions();
    let deviceParsersClosed = false;
    const closeDeviceMessageParsers = () => {
      if (deviceParsersClosed) return;
      deviceParsersClosed = true;
      sessionOptions.deviceMessageParsers.close();
    };
    serializerRef.current = new ScrcpyControlMessageSerializer(sessionOptions);
    const video = new PushReadableStream<Uint8Array>((controller) => {
      videoController = controller;
    });
    const deviceMessages = new PushReadableStream<Uint8Array>((controller) => {
      deviceMessageController = controller;
    });
    const bufferedDeviceMessages = new BufferedReadableStream(deviceMessages);
    const scheme = location.protocol === "https:" ? "wss" : "ws";
    const socket = new WebSocket(`${scheme}://${location.host}/api/screen`);
    socket.binaryType = "arraybuffer";
    socketRef.current = socket;
    setState("starting");
    setDetail("正在启动手机端视频编码器");
    setClipboardStatus("剪切板通道正在连接");

    const parseDeviceMessages = async () => {
      while (!disposed) {
        const id = (await bufferedDeviceMessages.readExactly(1))[0];
        await sessionOptions.deviceMessageParsers.parse(id, bufferedDeviceMessages);
      }
    };

    const receiveClipboard = async () => {
      const clipboard = sessionOptions.clipboard;
      if (!clipboard) return;
      const reader = clipboard.getReader();
      try {
        while (!disposed) {
          const { done, value } = await reader.read();
          if (done) return;
          setDeviceClipboard(value);
          if (
            window.isSecureContext &&
            navigator.clipboard?.writeText &&
            document.hasFocus()
          ) {
            try {
              await navigator.clipboard.writeText(value);
              if (!disposed) {
                setClipboardStatus(
                  `手机剪切板已自动同步到电脑（${value.length} 个字符）`,
                );
              }
              continue;
            } catch {
              // Fall back to the explicit copy button below.
            }
          }
          if (!disposed) {
            setClipboardStatus(
              `收到手机剪切板（${value.length} 个字符），点击“复制手机剪切板”`,
            );
          }
        }
      } finally {
        reader.releaseLock();
      }
    };

    parseDeviceMessages().catch((error) => {
      if (!disposed && !deviceMessagesEnded) {
        sessionOptions.deviceMessageParsers.error(error);
        setClipboardStatus("手机剪切板通道解析失败");
      }
    });
    receiveClipboard().catch(() => {
      if (!disposed) setClipboardStatus("手机剪切板通道已关闭");
    });

    const decode = async () => {
      const stream = await sessionOptions.parseVideoStreamMetadata(video);
      if (disposed || !canvasRef.current) return;
      if (stream.metadata.width && stream.metadata.height) {
        videoSizeRef.current = {
          width: stream.metadata.width,
          height: stream.metadata.height,
        };
      }
      const packets = stream.stream.pipeThrough(
        sessionOptions.createMediaStreamTransformer(),
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
          } else if (message.type === "ready") {
            setClipboardStatus("剪切板已连接 · 在画面中按 Ctrl+V 可粘贴到手机");
          }
        } catch {
          // Ignore unknown status messages; video always arrives as binary.
        }
        return;
      }
      if (!(event.data instanceof ArrayBuffer)) return;
      const payload = new Uint8Array(event.data);
      if (payload.length < 2) return;
      const chunk = payload.slice(1);
      if (payload[0] === 0 && videoController) {
        enqueueChain = enqueueChain
          .then(() => videoController?.enqueue(chunk))
          .then(() => undefined)
          .catch(() => undefined);
      } else if (payload[0] === 2 && deviceMessageController) {
        deviceMessageChain = deviceMessageChain
          .then(() => deviceMessageController?.enqueue(chunk))
          .then(() => undefined)
          .catch(() => undefined);
      }
    };
    socket.onerror = () => {
      if (!disposed) {
        setState("error");
        setDetail("无法建立屏幕控制通道");
      }
    };
    socket.onclose = () => {
      deviceMessagesEnded = true;
      enqueueChain.finally(() => {
        try {
          videoController?.close();
        } catch {
          // Decoder may already have closed the stream.
        }
      });
      deviceMessageChain.finally(() => {
        try {
          deviceMessageController?.close();
        } catch {
          // Device message parser may already have closed the stream.
        }
        closeDeviceMessageParsers();
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
      deviceMessagesEnded = true;
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
      deviceMessageChain.finally(() => {
        try {
          deviceMessageController?.close();
        } catch {
          // Stream already closed.
        }
      });
      closeDeviceMessageParsers();
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

  const pasteBrowserClipboard = async () => {
    stageRef.current?.focus();
    if (!window.isSecureContext || !navigator.clipboard?.readText) {
      setClipboardStatus("浏览器当前不允许直接读取，请在画面中按 Ctrl+V");
      return;
    }
    try {
      const text = await navigator.clipboard.readText();
      await sendClipboardToDevice(text, true);
    } catch {
      setClipboardStatus("读取电脑剪切板失败，请在画面中按 Ctrl+V");
    }
  };

  const copyDeviceClipboard = async () => {
    if (deviceClipboard === null) return;
    try {
      await writeBrowserClipboard(deviceClipboard);
      setClipboardStatus(
        `手机剪切板已复制到电脑（${deviceClipboard.length} 个字符）`,
      );
    } catch (error) {
      setClipboardStatus(
        error instanceof Error ? error.message : "复制手机剪切板失败",
      );
    }
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
            disabled={state !== "connected"}
            onClick={pasteBrowserClipboard}
            title="安全连接可直接读取；普通 HTTP 下请在画面中按 Ctrl+V"
          >
            粘贴到手机
          </button>
          <button
            type="button"
            disabled={deviceClipboard === null}
            onClick={copyDeviceClipboard}
          >
            复制手机剪切板
          </button>
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
        ref={stageRef}
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
      <footer className="terminal-help screen-help">
        <span role="status" aria-live="polite">{clipboardStatus}</span>
        <span>画面获得焦点后支持键盘输入；离开本页会自动停止编码。</span>
      </footer>
    </article>
  );
}
