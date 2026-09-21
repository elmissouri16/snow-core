//#region \0rolldown/runtime.js
var e = (e, t) => () => (t || (e((t = { exports: {} }).exports, t), e = null), t.exports), t = /* @__PURE__ */ e(((e) => {
	var t = Symbol.for("react.transitional.element"), n = Symbol.for("react.portal"), r = Symbol.for("react.fragment"), i = Symbol.for("react.strict_mode"), a = Symbol.for("react.profiler"), o = Symbol.for("react.consumer"), s = Symbol.for("react.context"), c = Symbol.for("react.forward_ref"), l = Symbol.for("react.suspense"), u = Symbol.for("react.memo"), d = Symbol.for("react.lazy"), f = Symbol.for("react.activity"), p = Symbol.for("react.view_transition"), m = Symbol.iterator;
	function h(e) {
		return typeof e != "object" || !e ? null : (e = m && e[m] || e["@@iterator"], typeof e == "function" ? e : null);
	}
	var g = {
		isMounted: function() {
			return !1;
		},
		enqueueForceUpdate: function() {},
		enqueueReplaceState: function() {},
		enqueueSetState: function() {}
	}, _ = Object.assign, v = {};
	function y(e, t, n) {
		this.props = e, this.context = t, this.refs = v, this.updater = n || g;
	}
	y.prototype.isReactComponent = {}, y.prototype.setState = function(e, t) {
		if (typeof e != "object" && typeof e != "function" && e != null) throw Error("takes an object of state variables to update or a function which returns an object of state variables.");
		this.updater.enqueueSetState(this, e, t, "setState");
	}, y.prototype.forceUpdate = function(e) {
		this.updater.enqueueForceUpdate(this, e, "forceUpdate");
	};
	function b() {}
	b.prototype = y.prototype;
	function x(e, t, n) {
		this.props = e, this.context = t, this.refs = v, this.updater = n || g;
	}
	var S = x.prototype = new b();
	S.constructor = x, _(S, y.prototype), S.isPureReactComponent = !0;
	var ee = Array.isArray;
	function C() {}
	var w = {
		H: null,
		A: null,
		T: null,
		S: null
	}, te = Object.prototype.hasOwnProperty;
	function T(e, n, r) {
		var i = r.ref;
		return {
			$$typeof: t,
			type: e,
			key: n,
			ref: i === void 0 ? null : i,
			props: r
		};
	}
	function ne(e, t) {
		return T(e.type, t, e.props);
	}
	function E(e) {
		return typeof e == "object" && !!e && e.$$typeof === t;
	}
	function re(e) {
		var t = {
			"=": "=0",
			":": "=2"
		};
		return "$" + e.replace(/[=:]/g, function(e) {
			return t[e];
		});
	}
	var ie = /\/+/g;
	function D(e, t) {
		return typeof e == "object" && e && e.key != null ? re("" + e.key) : t.toString(36);
	}
	function ae(e) {
		switch (e.status) {
			case "fulfilled": return e.value;
			case "rejected": throw e.reason;
			default: switch (typeof e.status == "string" ? e.then(C, C) : (e.status = "pending", e.then(function(t) {
				e.status === "pending" && (e.status = "fulfilled", e.value = t);
			}, function(t) {
				e.status === "pending" && (e.status = "rejected", e.reason = t);
			})), e.status) {
				case "fulfilled": return e.value;
				case "rejected": throw e.reason;
			}
		}
		throw e;
	}
	function oe(e, r, i, a, o) {
		var s = typeof e;
		(s === "undefined" || s === "boolean") && (e = null);
		var c = !1;
		if (e === null) c = !0;
		else switch (s) {
			case "bigint":
			case "string":
			case "number":
				c = !0;
				break;
			case "object": switch (e.$$typeof) {
				case t:
				case n:
					c = !0;
					break;
				case d: return c = e._init, oe(c(e._payload), r, i, a, o);
			}
		}
		if (c) return o = o(e), c = a === "" ? "." + D(e, 0) : a, ee(o) ? (i = "", c != null && (i = c.replace(ie, "$&/") + "/"), oe(o, r, i, "", function(e) {
			return e;
		})) : o != null && (E(o) && (o = ne(o, i + (o.key == null || e && e.key === o.key ? "" : ("" + o.key).replace(ie, "$&/") + "/") + c)), r.push(o)), 1;
		c = 0;
		var l = a === "" ? "." : a + ":";
		if (ee(e)) for (var u = 0; u < e.length; u++) a = e[u], s = l + D(a, u), c += oe(a, r, i, s, o);
		else if (u = h(e), typeof u == "function") for (e = u.call(e), u = 0; !(a = e.next()).done;) a = a.value, s = l + D(a, u++), c += oe(a, r, i, s, o);
		else if (s === "object") {
			if (typeof e.then == "function") return oe(ae(e), r, i, a, o);
			throw r = String(e), Error("Objects are not valid as a React child (found: " + (r === "[object Object]" ? "object with keys {" + Object.keys(e).join(", ") + "}" : r) + "). If you meant to render a collection of children, use an array instead.");
		}
		return c;
	}
	function se(e, t, n) {
		if (e == null) return e;
		var r = [], i = 0;
		return oe(e, r, "", "", function(e) {
			return t.call(n, e, i++);
		}), r;
	}
	function ce(e) {
		if (e._status === -1) {
			var t = e._result, n = t();
			n.then(function(t) {
				(e._status === 0 || e._status === -1) && (e._status = 1, e._result = t, n.status === void 0 && (n.status = "fulfilled", n.value = t));
			}, function(t) {
				(e._status === 0 || e._status === -1) && (e._status = 2, e._result = t, n.status === void 0 && (n.status = "rejected", n.reason = t));
			}), e._status === -1 && (e._status = 0, e._result = n);
		}
		if (e._status === 1) return e._result.default;
		throw e._result;
	}
	var le = typeof reportError == "function" ? reportError : function(e) {
		if (typeof window == "object" && typeof window.ErrorEvent == "function") {
			var t = new window.ErrorEvent("error", {
				bubbles: !0,
				cancelable: !0,
				message: typeof e == "object" && e && typeof e.message == "string" ? String(e.message) : String(e),
				error: e
			});
			if (!window.dispatchEvent(t)) return;
		} else if (typeof process == "object" && typeof process.emit == "function") {
			process.emit("uncaughtException", e);
			return;
		}
		console.error(e);
	};
	function ue(e) {
		var t = w.T, n = {};
		n.types = t === null ? null : t.types, w.T = n;
		try {
			var r = e(), i = w.S;
			i !== null && i(n, r), typeof r == "object" && r && typeof r.then == "function" && r.then(C, le);
		} catch (e) {
			le(e);
		} finally {
			t !== null && n.types !== null && (t.types = n.types), w.T = t;
		}
	}
	function de(e) {
		var t = w.T;
		if (t !== null) {
			var n = t.types;
			n === null ? t.types = [e] : n.indexOf(e) === -1 && n.push(e);
		} else ue(de.bind(null, e));
	}
	var fe = {
		map: se,
		forEach: function(e, t, n) {
			se(e, function() {
				t.apply(this, arguments);
			}, n);
		},
		count: function(e) {
			var t = 0;
			return se(e, function() {
				t++;
			}), t;
		},
		toArray: function(e) {
			return se(e, function(e) {
				return e;
			}) || [];
		},
		only: function(e) {
			if (!E(e)) throw Error("React.Children.only expected to receive a single React element child.");
			return e;
		}
	};
	e.Activity = f, e.Children = fe, e.Component = y, e.Fragment = r, e.Profiler = a, e.PureComponent = x, e.StrictMode = i, e.Suspense = l, e.ViewTransition = p, e.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE = w, e.__COMPILER_RUNTIME = {
		__proto__: null,
		c: function(e) {
			return w.H.useMemoCache(e);
		}
	}, e.addTransitionType = de, e.cache = function(e) {
		return function() {
			return e.apply(null, arguments);
		};
	}, e.cacheSignal = function() {
		return null;
	}, e.cloneElement = function(e, t, n) {
		if (e == null) throw Error("The argument must be a React element, but you passed " + e + ".");
		var r = _({}, e.props), i = e.key;
		if (t != null) for (a in t.key !== void 0 && (i = "" + t.key), t) !te.call(t, a) || a === "key" || a === "__self" || a === "__source" || a === "ref" && t.ref === void 0 || (r[a] = t[a]);
		var a = arguments.length - 2;
		if (a === 1) r.children = n;
		else if (1 < a) {
			for (var o = Array(a), s = 0; s < a; s++) o[s] = arguments[s + 2];
			r.children = o;
		}
		return T(e.type, i, r);
	}, e.createContext = function(e) {
		return e = {
			$$typeof: s,
			_currentValue: e,
			_currentValue2: e,
			_threadCount: 0,
			Provider: null,
			Consumer: null
		}, e.Provider = e, e.Consumer = {
			$$typeof: o,
			_context: e
		}, e;
	}, e.createElement = function(e, t, n) {
		var r, i = {}, a = null;
		if (t != null) for (r in t.key !== void 0 && (a = "" + t.key), t) te.call(t, r) && r !== "key" && r !== "__self" && r !== "__source" && (i[r] = t[r]);
		var o = arguments.length - 2;
		if (o === 1) i.children = n;
		else if (1 < o) {
			for (var s = Array(o), c = 0; c < o; c++) s[c] = arguments[c + 2];
			i.children = s;
		}
		if (e && e.defaultProps) for (r in o = e.defaultProps, o) i[r] === void 0 && (i[r] = o[r]);
		return T(e, a, i);
	}, e.createRef = function() {
		return { current: null };
	}, e.forwardRef = function(e) {
		return {
			$$typeof: c,
			render: e
		};
	}, e.isValidElement = E, e.lazy = function(e) {
		return {
			$$typeof: d,
			_payload: {
				_status: -1,
				_result: e
			},
			_init: ce
		};
	}, e.memo = function(e, t) {
		return {
			$$typeof: u,
			type: e,
			compare: t === void 0 ? null : t
		};
	}, e.startTransition = ue, e.unstable_useCacheRefresh = function() {
		return w.H.useCacheRefresh();
	}, e.use = function(e) {
		return w.H.use(e);
	}, e.useActionState = function(e, t, n) {
		return w.H.useActionState(e, t, n);
	}, e.useCallback = function(e, t) {
		return w.H.useCallback(e, t);
	}, e.useContext = function(e) {
		return w.H.useContext(e);
	}, e.useDebugValue = function() {}, e.useDeferredValue = function(e, t) {
		return w.H.useDeferredValue(e, t);
	}, e.useEffect = function(e, t) {
		return w.H.useEffect(e, t);
	}, e.useEffectEvent = function(e) {
		return w.H.useEffectEvent(e);
	}, e.useId = function() {
		return w.H.useId();
	}, e.useImperativeHandle = function(e, t, n) {
		return w.H.useImperativeHandle(e, t, n);
	}, e.useInsertionEffect = function(e, t) {
		return w.H.useInsertionEffect(e, t);
	}, e.useLayoutEffect = function(e, t) {
		return w.H.useLayoutEffect(e, t);
	}, e.useMemo = function(e, t) {
		return w.H.useMemo(e, t);
	}, e.useOptimistic = function(e, t) {
		return w.H.useOptimistic(e, t);
	}, e.useReducer = function(e, t, n) {
		return w.H.useReducer(e, t, n);
	}, e.useRef = function(e) {
		return w.H.useRef(e);
	}, e.useState = function(e) {
		return w.H.useState(e);
	}, e.useSyncExternalStore = function(e, t, n) {
		return w.H.useSyncExternalStore(e, t, n);
	}, e.useTransition = function() {
		return w.H.useTransition();
	}, e.version = "19.3.0";
})), n = /* @__PURE__ */ e(((e, n) => {
	n.exports = t();
})), r = /* @__PURE__ */ e(((e) => {
	function t(e, t) {
		var n = e.length;
		e.push(t);
		a: for (; 0 < n;) {
			var r = n - 1 >>> 1, a = e[r];
			if (0 < i(a, t)) e[r] = t, e[n] = a, n = r;
			else break a;
		}
	}
	function n(e) {
		return e.length === 0 ? null : e[0];
	}
	function r(e) {
		if (e.length === 0) return null;
		var t = e[0], n = e.pop();
		if (n !== t) {
			e[0] = n;
			a: for (var r = 0, a = e.length, o = a >>> 1; r < o;) {
				var s = 2 * (r + 1) - 1, c = e[s], l = s + 1, u = e[l];
				if (0 > i(c, n)) l < a && 0 > i(u, c) ? (e[r] = u, e[l] = n, r = l) : (e[r] = c, e[s] = n, r = s);
				else if (l < a && 0 > i(u, n)) e[r] = u, e[l] = n, r = l;
				else break a;
			}
		}
		return t;
	}
	function i(e, t) {
		var n = e.sortIndex - t.sortIndex;
		return n === 0 ? e.id - t.id : n;
	}
	if (e.unstable_now = void 0, typeof performance == "object" && typeof performance.now == "function") {
		var a = performance;
		e.unstable_now = function() {
			return a.now();
		};
	} else {
		var o = Date, s = o.now();
		e.unstable_now = function() {
			return o.now() - s;
		};
	}
	var c = [], l = [], u = 1, d = null, f = 3, p = !1, m = !1, h = !1, g = !1, _ = typeof setTimeout == "function" ? setTimeout : null, v = typeof clearTimeout == "function" ? clearTimeout : null, y = typeof setImmediate < "u" ? setImmediate : null;
	function b(e) {
		for (var i = n(l); i !== null;) {
			if (i.callback === null) r(l);
			else if (i.startTime <= e) r(l), i.sortIndex = i.expirationTime, t(c, i);
			else break;
			i = n(l);
		}
	}
	function x(e) {
		if (h = !1, b(e), !m) {
			if (n(c) !== null) m = !0, S || (S = !0, ne());
			else {
				var t = n(l);
				t !== null && ie(x, t.startTime - e);
			}
		}
	}
	var S = !1, ee = -1, C = 5, w = -1;
	function te() {
		return g ? !0 : !(e.unstable_now() - w < C);
	}
	function T() {
		if (g = !1, S) {
			var t = e.unstable_now();
			w = t;
			var i = !0;
			try {
				a: {
					m = !1, h && (h = !1, v(ee), ee = -1), p = !0;
					var a = f;
					try {
						b: {
							for (b(t), d = n(c); d !== null && !(d.expirationTime > t && te());) {
								var o = d.callback;
								if (typeof o == "function") {
									d.callback = null, f = d.priorityLevel;
									var s = o(d.expirationTime <= t);
									if (t = e.unstable_now(), typeof s == "function") {
										d.callback = s, b(t), i = !0;
										break b;
									}
									d === n(c) && r(c), b(t);
								} else r(c);
								d = n(c);
							}
							if (d !== null) i = !0;
							else {
								var u = n(l);
								u !== null && ie(x, u.startTime - t), i = !1;
							}
						}
						break a;
					} finally {
						d = null, f = a, p = !1;
					}
					i = void 0;
				}
			} finally {
				i ? ne() : S = !1;
			}
		}
	}
	var ne;
	if (typeof y == "function") ne = function() {
		y(T);
	};
	else if (typeof MessageChannel < "u") {
		var E = new MessageChannel(), re = E.port2;
		E.port1.onmessage = T, ne = function() {
			re.postMessage(null);
		};
	} else ne = function() {
		_(T, 0);
	};
	function ie(t, n) {
		ee = _(function() {
			t(e.unstable_now());
		}, n);
	}
	e.unstable_IdlePriority = 5, e.unstable_ImmediatePriority = 1, e.unstable_LowPriority = 4, e.unstable_NormalPriority = 3, e.unstable_Profiling = null, e.unstable_UserBlockingPriority = 2, e.unstable_cancelCallback = function(e) {
		e.callback = null;
	}, e.unstable_forceFrameRate = function(e) {
		0 > e || 125 < e ? console.error("forceFrameRate takes a positive int between 0 and 125, forcing frame rates higher than 125 fps is not supported") : C = 0 < e ? Math.floor(1e3 / e) : 5;
	}, e.unstable_getCurrentPriorityLevel = function() {
		return f;
	}, e.unstable_next = function(e) {
		switch (f) {
			case 1:
			case 2:
			case 3:
				var t = 3;
				break;
			default: t = f;
		}
		var n = f;
		f = t;
		try {
			return e();
		} finally {
			f = n;
		}
	}, e.unstable_requestPaint = function() {
		g = !0;
	}, e.unstable_runWithPriority = function(e, t) {
		switch (e) {
			case 1:
			case 2:
			case 3:
			case 4:
			case 5: break;
			default: e = 3;
		}
		var n = f;
		f = e;
		try {
			return t();
		} finally {
			f = n;
		}
	}, e.unstable_scheduleCallback = function(r, i, a) {
		var o = e.unstable_now();
		switch (typeof a == "object" && a ? (a = a.delay, a = typeof a == "number" && 0 < a ? o + a : o) : a = o, r) {
			case 1:
				var s = -1;
				break;
			case 2:
				s = 250;
				break;
			case 5:
				s = 1073741823;
				break;
			case 4:
				s = 1e4;
				break;
			default: s = 5e3;
		}
		return s = a + s, r = {
			id: u++,
			callback: i,
			priorityLevel: r,
			startTime: a,
			expirationTime: s,
			sortIndex: -1
		}, a > o ? (r.sortIndex = a, t(l, r), n(c) === null && r === n(l) && (h ? (v(ee), ee = -1) : h = !0, ie(x, a - o))) : (r.sortIndex = s, t(c, r), m || p || (m = !0, S || (S = !0, ne()))), r;
	}, e.unstable_shouldYield = te, e.unstable_wrapCallback = function(e) {
		var t = f;
		return function() {
			var n = f;
			f = t;
			try {
				return e.apply(this, arguments);
			} finally {
				f = n;
			}
		};
	};
})), i = /* @__PURE__ */ e(((e, t) => {
	t.exports = r();
})), a = /* @__PURE__ */ e(((e) => {
	var t = n();
	function r(e) {
		var t = "https://react.dev/errors/" + e;
		if (1 < arguments.length) {
			t += "?args[]=" + encodeURIComponent(arguments[1]);
			for (var n = 2; n < arguments.length; n++) t += "&args[]=" + encodeURIComponent(arguments[n]);
		}
		return "Minified React error #" + e + "; visit " + t + " for the full message or use the non-minified dev environment for full errors and additional helpful warnings.";
	}
	function i() {}
	var a = {
		d: {
			f: i,
			r: function() {
				throw Error(r(522));
			},
			D: i,
			C: i,
			L: i,
			m: i,
			X: i,
			S: i,
			M: i
		},
		p: 0,
		findDOMNode: null
	}, o = Symbol.for("react.portal"), s = Symbol.for("react.recoverable"), c = Symbol.for("react.optimistic_key");
	function l(e, t, n) {
		var r = 3 < arguments.length && arguments[3] !== void 0 ? arguments[3] : null;
		return {
			$$typeof: o,
			key: r == null ? null : r === c ? c : "" + r,
			children: e,
			containerInfo: t,
			implementation: n
		};
	}
	var u = t.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE;
	function d(e, t) {
		if (e === "font") return "";
		if (typeof t == "string") return t === "use-credentials" ? t : "";
	}
	e.__DOM_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE = a, e.browser = function(e) {
		return {
			$$typeof: s,
			_reason: e
		};
	}, e.createPortal = function(e, t) {
		var n = 2 < arguments.length && arguments[2] !== void 0 ? arguments[2] : null;
		if (!t || t.nodeType !== 1 && t.nodeType !== 9 && t.nodeType !== 11) throw Error(r(299));
		return l(e, t, null, n);
	}, e.flushSync = function(e) {
		var t = u.T, n = a.p;
		try {
			if (u.T = null, a.p = 2, e) return e();
		} finally {
			u.T = t, a.p = n, a.d.f();
		}
	}, e.preconnect = function(e, t) {
		typeof e == "string" && (t ? (t = t.crossOrigin, t = typeof t == "string" ? t === "use-credentials" ? t : "" : void 0) : t = null, a.d.C(e, t));
	}, e.prefetchDNS = function(e) {
		typeof e == "string" && a.d.D(e);
	}, e.preinit = function(e, t) {
		if (typeof e == "string" && t && typeof t.as == "string") {
			var n = t.as, r = d(n, t.crossOrigin), i = typeof t.integrity == "string" ? t.integrity : void 0, o = typeof t.fetchPriority == "string" ? t.fetchPriority : void 0;
			n === "style" ? a.d.S(e, typeof t.precedence == "string" ? t.precedence : void 0, {
				crossOrigin: r,
				integrity: i,
				fetchPriority: o
			}) : n === "script" && a.d.X(e, {
				crossOrigin: r,
				integrity: i,
				fetchPriority: o,
				nonce: typeof t.nonce == "string" ? t.nonce : void 0
			});
		}
	}, e.preinitModule = function(e, t) {
		if (typeof e == "string") {
			if (typeof t == "object" && t) {
				if (t.as == null || t.as === "script") {
					var n = d(t.as, t.crossOrigin);
					a.d.M(e, {
						crossOrigin: n,
						integrity: typeof t.integrity == "string" ? t.integrity : void 0,
						nonce: typeof t.nonce == "string" ? t.nonce : void 0,
						fetchPriority: typeof t.fetchPriority == "string" ? t.fetchPriority : void 0
					});
				}
			} else t ?? a.d.M(e);
		}
	}, e.preload = function(e, t) {
		if (typeof e == "string" && typeof t == "object" && t && typeof t.as == "string") {
			var n = t.as, r = d(n, t.crossOrigin);
			a.d.L(e, n, {
				crossOrigin: r,
				integrity: typeof t.integrity == "string" ? t.integrity : void 0,
				nonce: typeof t.nonce == "string" ? t.nonce : void 0,
				type: typeof t.type == "string" ? t.type : void 0,
				fetchPriority: typeof t.fetchPriority == "string" ? t.fetchPriority : void 0,
				referrerPolicy: typeof t.referrerPolicy == "string" ? t.referrerPolicy : void 0,
				imageSrcSet: typeof t.imageSrcSet == "string" ? t.imageSrcSet : void 0,
				imageSizes: typeof t.imageSizes == "string" ? t.imageSizes : void 0,
				media: typeof t.media == "string" ? t.media : void 0
			});
		}
	}, e.preloadModule = function(e, t) {
		if (typeof e == "string") {
			if (t) {
				var n = d(t.as, t.crossOrigin);
				a.d.m(e, {
					as: typeof t.as == "string" && t.as !== "script" ? t.as : void 0,
					crossOrigin: n,
					integrity: typeof t.integrity == "string" ? t.integrity : void 0,
					nonce: typeof t.nonce == "string" ? t.nonce : void 0,
					fetchPriority: typeof t.fetchPriority == "string" ? t.fetchPriority : void 0
				});
			} else a.d.m(e);
		}
	}, e.requestFormReset = function(e) {
		a.d.r(e);
	}, e.unstable_batchedUpdates = function(e, t) {
		return e(t);
	}, e.useFormState = function(e, t, n) {
		return u.H.useFormState(e, t, n);
	}, e.useFormStatus = function() {
		return u.H.useHostTransitionStatus();
	}, e.version = "19.3.0";
})), o = /* @__PURE__ */ e(((e, t) => {
	function n() {
		if (!(typeof __REACT_DEVTOOLS_GLOBAL_HOOK__ > "u" || typeof __REACT_DEVTOOLS_GLOBAL_HOOK__.checkDCE != "function")) try {
			__REACT_DEVTOOLS_GLOBAL_HOOK__.checkDCE(n);
		} catch (e) {
			console.error(e);
		}
	}
	n(), t.exports = a();
})), s = /* @__PURE__ */ e(((e) => {
	var t = i(), r = n(), a = o();
	function s(e) {
		var t = "https://react.dev/errors/" + e;
		if (1 < arguments.length) {
			t += "?args[]=" + encodeURIComponent(arguments[1]);
			for (var n = 2; n < arguments.length; n++) t += "&args[]=" + encodeURIComponent(arguments[n]);
		}
		return "Minified React error #" + e + "; visit " + t + " for the full message or use the non-minified dev environment for full errors and additional helpful warnings.";
	}
	function c(e) {
		return !(!e || e.nodeType !== 1 && e.nodeType !== 9 && e.nodeType !== 11);
	}
	function l(e) {
		for (var t = e, n = t; n && !n.alternate;) t = n, t.flags & 4098 && (e = t.return), n = t.return;
		for (; t.return;) t = t.return;
		return t.tag === 3 ? e : null;
	}
	function u(e) {
		if (e.tag === 13) {
			var t = e.memoizedState;
			if (t === null && (e = e.alternate, e !== null && (t = e.memoizedState)), t !== null) return t.dehydrated;
		}
		return null;
	}
	function d(e) {
		if (e.tag === 31) {
			var t = e.memoizedState;
			if (t === null && (e = e.alternate, e !== null && (t = e.memoizedState)), t !== null) return t.dehydrated;
		}
		return null;
	}
	function f(e) {
		if (l(e) !== e) throw Error(s(188));
	}
	function p(e) {
		var t = e.alternate;
		if (!t) {
			if (t = l(e), t === null) throw Error(s(188));
			return t === e ? e : null;
		}
		for (var n = e, r = t;;) {
			var i = n.return;
			if (i === null) break;
			var a = i.alternate;
			if (a === null) {
				if (r = i.return, r !== null) {
					n = r;
					continue;
				}
				break;
			}
			if (i.child === a.child) {
				for (a = i.child; a;) {
					if (a === n) return f(i), e;
					if (a === r) return f(i), t;
					a = a.sibling;
				}
				throw Error(s(188));
			}
			if (n.return !== r.return) n = i, r = a;
			else {
				for (var o = !1, c = i.child; c;) {
					if (c === n) {
						o = !0, n = i, r = a;
						break;
					}
					if (c === r) {
						o = !0, r = i, n = a;
						break;
					}
					c = c.sibling;
				}
				if (!o) {
					for (c = a.child; c;) {
						if (c === n) {
							o = !0, n = a, r = i;
							break;
						}
						if (c === r) {
							o = !0, r = a, n = i;
							break;
						}
						c = c.sibling;
					}
					if (!o) throw Error(s(189));
				}
			}
			if (n.alternate !== r) throw Error(s(190));
		}
		if (n.tag !== 3) throw Error(s(188));
		return n.stateNode.current === n ? e : t;
	}
	function m(e) {
		var t = e.tag;
		if (t === 5 || t === 26 || t === 27 || t === 6) return e;
		for (e = e.child; e !== null;) {
			if (t = m(e), t !== null) return t;
			e = e.sibling;
		}
		return null;
	}
	function h(e, t, n, r, i, a) {
		for (; e !== null;) {
			if ((e.tag === 5 || e.tag === 27 || e.tag === 6) && n(e, r, i, a) || (e.tag !== 22 || e.memoizedState === null) && (t || e.tag !== 5 && e.tag !== 27) && h(e.child, t, n, r, i, a)) return !0;
			e = e.sibling;
		}
		return !1;
	}
	function g(e) {
		for (e = e.return; e !== null;) {
			if (e.tag === 3 || e.tag === 5 || e.tag === 27) return e;
			e = e.return;
		}
		return null;
	}
	function _(e) {
		var t = !1;
		for (e = e.return; e !== null && (e.tag === 4 && (t = !0), e.tag !== 3 && e.tag !== 5 && e.tag !== 27);) e = e.return;
		return t;
	}
	function v(e) {
		var t = [null, null], n = g(e);
		return n === null || y(t, e, n.child, { foundSelf: !1 }), t;
	}
	function y(e, t, n, r) {
		for (; n !== null;) {
			if (n === t) r.foundSelf = !0;
			else if (n.tag === 5 || n.tag === 27 || n.tag === 6) {
				if (r.foundSelf) return e[1] = n, !0;
				e[0] = n;
			} else if ((n.tag !== 22 || n.memoizedState === null) && y(e, t, n.child, r)) return !0;
			n = n.sibling;
		}
		return !1;
	}
	function b(e) {
		switch (e.tag) {
			case 5:
			case 27:
			case 6: return e.stateNode;
			case 3: return e.stateNode.containerInfo;
			default: throw Error(s(559));
		}
	}
	var x = null, S = null;
	function ee(e, t, n) {
		return e === n || e === t && (x = e, !0);
	}
	function C(e, t, n) {
		return e === n ? (S = e, !1) : e === t && (S !== null && (x = e), !0);
	}
	function w(e) {
		if (e === null) return null;
		do
			e = e === null ? null : e.return;
		while (e && e.tag !== 5 && e.tag !== 27 && e.tag !== 3);
		return e || null;
	}
	function te(e, t, n) {
		for (var r = 0, i = e; i; i = n(i)) r++;
		i = 0;
		for (var a = t; a; a = n(a)) i++;
		for (; 0 < r - i;) e = n(e), r--;
		for (; 0 < i - r;) t = n(t), i--;
		for (; r--;) {
			if (e === t || t !== null && e === t.alternate) return e;
			e = n(e), t = n(t);
		}
		return null;
	}
	var T = Object.assign, ne = Symbol.for("react.element"), E = Symbol.for("react.transitional.element"), re = Symbol.for("react.portal"), ie = Symbol.for("react.fragment"), D = Symbol.for("react.strict_mode"), ae = Symbol.for("react.profiler"), oe = Symbol.for("react.consumer"), se = Symbol.for("react.context"), ce = Symbol.for("react.forward_ref"), le = Symbol.for("react.suspense"), ue = Symbol.for("react.suspense_list"), de = Symbol.for("react.memo"), fe = Symbol.for("react.lazy"), pe = Symbol.for("react.activity"), me = Symbol.for("react.legacy_hidden"), he = Symbol.for("react.memo_cache_sentinel"), ge = Symbol.for("react.view_transition"), _e = Symbol.for("react.recoverable"), ve = Symbol.iterator;
	function ye(e) {
		return typeof e != "object" || !e ? null : (e = ve && e[ve] || e["@@iterator"], typeof e == "function" ? e : null);
	}
	var be = Symbol.for("react.client.reference");
	function xe(e) {
		if (e == null) return null;
		if (typeof e == "function") return e.$$typeof === be ? null : e.displayName || e.name || null;
		if (typeof e == "string") return e;
		switch (e) {
			case ie: return "Fragment";
			case ae: return "Profiler";
			case D: return "StrictMode";
			case le: return "Suspense";
			case ue: return "SuspenseList";
			case pe: return "Activity";
			case ge: return "ViewTransition";
		}
		if (typeof e == "object") switch (e.$$typeof) {
			case re: return "Portal";
			case se: return e.displayName || "Context";
			case oe: return (e._context.displayName || "Context") + ".Consumer";
			case ce:
				var t = e.render;
				return e = e.displayName, e ||= (e = t.displayName || t.name || "", e === "" ? "ForwardRef" : "ForwardRef(" + e + ")"), e;
			case de: return t = e.displayName || null, t === null ? xe(e.type) || "Memo" : t;
			case fe:
				t = e._payload, e = e._init;
				try {
					return xe(e(t));
				} catch {}
		}
		return null;
	}
	var Se = Array.isArray, O = r.__CLIENT_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE, k = a.__DOM_INTERNALS_DO_NOT_USE_OR_WARN_USERS_THEY_CANNOT_UPGRADE, Ce = {
		pending: !1,
		data: null,
		method: null,
		action: null
	}, we = [], Te = -1;
	function Ee(e) {
		return { current: e };
	}
	function De(e) {
		0 > Te || (e.current = we[Te], we[Te] = null, Te--);
	}
	function A(e, t) {
		Te++, we[Te] = e.current, e.current = t;
	}
	var Oe = Ee(null), ke = Ee(null), Ae = Ee(null), je = Ee(null);
	function Me(e, t) {
		switch (A(Ae, t), A(ke, e), A(Oe, null), t.nodeType) {
			case 9:
			case 11:
				e = (e = t.documentElement) && (e = e.namespaceURI) ? up(e) : 0;
				break;
			default: if (e = t.tagName, t = t.namespaceURI) t = up(t), e = dp(t, e);
			else switch (e) {
				case "svg":
					e = 1;
					break;
				case "math":
					e = 2;
					break;
				default: e = 0;
			}
		}
		De(Oe), A(Oe, e);
	}
	function Ne() {
		De(Oe), De(ke), De(Ae);
	}
	function Pe(e) {
		var t = e.memoizedState;
		t !== null && (sh._currentValue = t.memoizedState, A(je, e)), t = Oe.current;
		var n = dp(t, e.type);
		t !== n && (A(ke, e), A(Oe, n));
	}
	function Fe(e) {
		ke.current === e && (De(Oe), De(ke)), je.current === e && (De(je), sh._currentValue = Ce);
	}
	var Ie, Le;
	function Re(e) {
		if (Ie === void 0) try {
			throw Error();
		} catch (e) {
			var t = e.stack.trim().match(/\n( *(at )?)/);
			Ie = t && t[1] || "", Le = -1 < e.stack.indexOf("\n    at") ? " (<anonymous>)" : -1 < e.stack.indexOf("@") ? "@unknown:0:0" : "";
		}
		return "\n" + Ie + e + Le;
	}
	var ze = !1;
	function Be(e, t) {
		if (!e || ze) return "";
		ze = !0;
		var n = Error.prepareStackTrace;
		Error.prepareStackTrace = void 0;
		try {
			var r = { DetermineComponentFrameRoot: function() {
				try {
					if (t) {
						var n = function() {
							throw Error();
						};
						if (Object.defineProperty(n.prototype, "props", { set: function() {
							throw Error();
						} }), typeof Reflect == "object" && Reflect.construct) {
							try {
								Reflect.construct(n, []);
							} catch (e) {
								var r = e;
							}
							Reflect.construct(e, [], n);
						} else {
							try {
								n.call();
							} catch (e) {
								r = e;
							}
							n = !1;
							try {
								var i = Object.getOwnPropertyDescriptor(e.prototype, "props");
								Object.defineProperty(e.prototype, "props", {
									configurable: !0,
									set: function() {
										throw Error();
									}
								}), n = !0, new e();
							} finally {
								n && (i === void 0 ? delete e.prototype.props : Object.defineProperty(e.prototype, "props", i));
							}
						}
					} else {
						try {
							throw Error();
						} catch (e) {
							r = e;
						}
						(n = e()) && typeof n.catch == "function" && n.catch(function() {});
					}
				} catch (e) {
					if (e && r && typeof e.stack == "string") return [e.stack, r.stack];
				}
				return [null, null];
			} };
			r.DetermineComponentFrameRoot.displayName = "DetermineComponentFrameRoot";
			var i = Object.getOwnPropertyDescriptor(r.DetermineComponentFrameRoot, "name");
			i && i.configurable && Object.defineProperty(r.DetermineComponentFrameRoot, "name", { value: "DetermineComponentFrameRoot" });
			var a = r.DetermineComponentFrameRoot(), o = a[0], s = a[1];
			if (o && s) {
				var c = o.split("\n"), l = s.split("\n");
				for (i = r = 0; r < c.length && !c[r].includes("DetermineComponentFrameRoot");) r++;
				for (; i < l.length && !l[i].includes("DetermineComponentFrameRoot");) i++;
				if (r === c.length || i === l.length) for (r = c.length - 1, i = l.length - 1; 1 <= r && 0 <= i && c[r] !== l[i];) i--;
				for (; 1 <= r && 0 <= i; r--, i--) if (c[r] !== l[i]) {
					if (r !== 1 || i !== 1) do
						if (r--, i--, 0 > i || c[r] !== l[i]) {
							var u = "\n" + c[r].replace(" at new ", " at ");
							return e.displayName && u.includes("<anonymous>") && (u = u.replace("<anonymous>", e.displayName)), u;
						}
					while (1 <= r && 0 <= i);
					break;
				}
			}
		} finally {
			ze = !1, Error.prepareStackTrace = n;
		}
		return (n = e ? e.displayName || e.name : "") ? Re(n) : "";
	}
	function Ve(e, t) {
		switch (e.tag) {
			case 26:
			case 27:
			case 5: return Re(e.type);
			case 16: return Re("Lazy");
			case 13: return e.child !== t && t !== null ? Re("Suspense Fallback") : Re("Suspense");
			case 19: return Re("SuspenseList");
			case 0:
			case 15: return Be(e.type, !1);
			case 11: return Be(e.type.render, !1);
			case 1: return Be(e.type, !0);
			case 31: return Re("Activity");
			case 30: return Re("ViewTransition");
			default: return "";
		}
	}
	function He(e) {
		try {
			var t = "", n = null;
			do
				t += Ve(e, n), n = e, e = e.return;
			while (e);
			return t;
		} catch (e) {
			return "\nError generating stack: " + e.message + "\n" + e.stack;
		}
	}
	var Ue = Object.prototype.hasOwnProperty, We = t.unstable_scheduleCallback, Ge = t.unstable_cancelCallback, Ke = t.unstable_shouldYield, qe = t.unstable_requestPaint, Je = t.unstable_now, Ye = t.unstable_getCurrentPriorityLevel, Xe = t.unstable_ImmediatePriority, Ze = t.unstable_UserBlockingPriority, Qe = t.unstable_NormalPriority, $e = t.unstable_LowPriority, et = t.unstable_IdlePriority, tt = t.log, nt = t.unstable_setDisableYieldValue, rt = null, it = null;
	function at(e) {
		if (typeof tt == "function" && nt(e), it && typeof it.setStrictMode == "function") try {
			it.setStrictMode(rt, e);
		} catch {}
	}
	var ot = Math.clz32 ? Math.clz32 : lt, st = Math.log, ct = Math.LN2;
	function lt(e) {
		return e >>>= 0, e === 0 ? 32 : 31 - (st(e) / ct | 0) | 0;
	}
	var ut = 256, dt = 262144, ft = 4194304;
	function pt(e) {
		var t = e & 42;
		if (t !== 0) return t;
		switch (e & -e) {
			case 1: return 1;
			case 2: return 2;
			case 4: return 4;
			case 8: return 8;
			case 16: return 16;
			case 32: return 32;
			case 64: return 64;
			case 128: return 128;
			case 256:
			case 512:
			case 1024:
			case 2048:
			case 4096:
			case 8192:
			case 16384:
			case 32768:
			case 65536:
			case 131072: return e & -e;
			case 262144:
			case 524288:
			case 1048576:
			case 2097152: return e & 3932160;
			case 4194304:
			case 8388608:
			case 16777216:
			case 33554432: return e & 62914560;
			case 67108864: return 67108864;
			case 134217728: return 134217728;
			case 268435456: return 268435456;
			case 536870912: return 536870912;
			case 1073741824: return 0;
			default: return e;
		}
	}
	function mt(e, t, n) {
		var r = e.pendingLanes;
		if (r === 0) return 0;
		var i = 0, a = e.suspendedLanes, o = e.pingedLanes;
		e = e.warmLanes;
		var s = r & 134217727;
		return s === 0 ? (s = r & ~a, s === 0 ? o === 0 ? n || (n = r & ~e, n !== 0 && (i = pt(n))) : i = pt(o) : i = pt(s)) : (r = s & ~a, r === 0 ? (o &= s, o === 0 ? n || (n = s & ~e, n !== 0 && (i = pt(n))) : i = pt(o)) : i = pt(r)), i === 0 ? 0 : t !== 0 && t !== i && (t & a) === 0 && (a = i & -i, n = t & -t, a >= n || a === 32 && n & 4194048) ? t : i;
	}
	function ht(e, t) {
		return (e.pendingLanes & ~(e.suspendedLanes & ~e.pingedLanes) & t) === 0;
	}
	function gt(e, t) {
		t & 8 && (t |= t & 32);
		var n = e.entangledLanes;
		if (n !== 0) for (e = e.entanglements, n &= t; 0 < n;) {
			var r = 31 - ot(n), i = 1 << r;
			t |= e[r], n &= ~i;
		}
		return t;
	}
	function _t(e, t) {
		switch (e) {
			case 1:
			case 2:
			case 4:
			case 8:
			case 64: return t + 250;
			case 16:
			case 32:
			case 128:
			case 256:
			case 512:
			case 1024:
			case 2048:
			case 4096:
			case 8192:
			case 16384:
			case 32768:
			case 65536:
			case 131072:
			case 262144:
			case 524288:
			case 1048576:
			case 2097152: return t + 5e3;
			case 4194304:
			case 8388608:
			case 16777216:
			case 33554432: return -1;
			case 67108864:
			case 134217728:
			case 268435456:
			case 536870912:
			case 1073741824: return -1;
			default: return -1;
		}
	}
	function vt() {
		var e = ft;
		return ft <<= 1, !(ft & 62914560) && (ft = 4194304), e;
	}
	function yt(e) {
		for (var t = [], n = 0; 31 > n; n++) t.push(e);
		return t;
	}
	function bt(e, t) {
		e.pendingLanes |= t, t !== 268435456 && (e.suspendedLanes = 0, e.pingedLanes = 0, e.warmLanes = 0);
	}
	function xt(e, t, n, r, i, a) {
		var o = e.pendingLanes;
		e.pendingLanes = n, e.suspendedLanes = 0, e.pingedLanes = 0, e.warmLanes = 0, e.expiredLanes &= n, e.entangledLanes &= n, e.errorRecoveryDisabledLanes &= n, e.shellSuspendCounter = 0;
		var s = e.entanglements, c = e.expirationTimes, l = e.hiddenUpdates;
		for (n = o & ~n; 0 < n;) {
			var u = 31 - ot(n), d = 1 << u;
			s[u] = 0, c[u] = -1;
			var f = l[u];
			if (f !== null) for (l[u] = null, u = 0; u < f.length; u++) {
				var p = f[u];
				p !== null && (p.lane &= -536870913);
			}
			n &= ~d;
		}
		r !== 0 && St(e, r, 0), a !== 0 && i === 0 && e.tag !== 0 && (e.suspendedLanes |= a & ~(o & ~t));
	}
	function St(e, t, n) {
		e.pendingLanes |= t, e.suspendedLanes &= ~t;
		var r = 31 - ot(t);
		e.entangledLanes |= t, e.entanglements[r] = e.entanglements[r] | 1073741824 | n & 261930;
	}
	function Ct(e, t) {
		var n = e.entangledLanes |= t;
		for (e = e.entanglements; n;) {
			var r = 31 - ot(n), i = 1 << r;
			i & t | e[r] & t && (e[r] |= t), n &= ~i;
		}
	}
	function wt(e, t) {
		var n = t & -t;
		return n = n & 42 ? 1 : Tt(n), (n & (e.suspendedLanes | t)) === 0 ? n : 0;
	}
	function Tt(e) {
		switch (e) {
			case 2:
				e = 1;
				break;
			case 8:
				e = 4;
				break;
			case 32:
				e = 16;
				break;
			case 256:
			case 512:
			case 1024:
			case 2048:
			case 4096:
			case 8192:
			case 16384:
			case 32768:
			case 65536:
			case 131072:
			case 262144:
			case 524288:
			case 1048576:
			case 2097152:
			case 4194304:
			case 8388608:
			case 16777216:
			case 33554432:
				e = 128;
				break;
			case 268435456:
				e = 134217728;
				break;
			default: e = 0;
		}
		return e;
	}
	function Et(e) {
		return e &= -e, 2 < e ? 8 < e ? e & 134217727 ? 32 : 268435456 : 8 : 2;
	}
	function Dt() {
		var e = k.p;
		return e === 0 ? (e = window.event, e === void 0 ? 32 : Ch(e.type)) : e;
	}
	function Ot(e, t) {
		var n = k.p;
		try {
			return k.p = e, t();
		} finally {
			k.p = n;
		}
	}
	var kt = Math.random().toString(36).slice(2), At = "__reactFiber$" + kt, jt = "__reactProps$" + kt, Mt = "__reactContainer$" + kt, Nt = "__reactEvents$" + kt, Pt = "__reactListeners$" + kt, Ft = "__reactHandles$" + kt, It = "__reactResources$" + kt, Lt = "__reactMarker$" + kt, Rt = "__reactLoad$" + kt;
	function zt(e) {
		delete e[At], delete e[jt], delete e[Pt], delete e[Ft];
	}
	function Bt(e) {
		var t;
		if (t = e[At]) return t;
		for (var n = e.parentNode; n;) {
			if (t = n[Mt] || n[At]) {
				if (n = t.alternate, t.child !== null || n !== null && n.child !== null) for (e = fm(e); e !== null;) {
					if (n = e[At]) return n;
					e = fm(e);
				}
				return t;
			}
			e = n, n = e.parentNode;
		}
		return null;
	}
	function Vt(e) {
		if (e = e[At] || e[Mt]) {
			var t = e.tag;
			if (t === 5 || t === 6 || t === 13 || t === 31 || t === 26 || t === 27 || t === 3) return e;
		}
		return null;
	}
	function Ht(e) {
		var t = e.tag;
		if (t === 5 || t === 26 || t === 27 || t === 6) return e.stateNode;
		throw Error(s(33));
	}
	function Ut(e) {
		var t = e[It];
		return t ||= e[It] = {
			hoistableStyles: /* @__PURE__ */ new Map(),
			hoistableScripts: /* @__PURE__ */ new Map()
		}, t;
	}
	function Wt(e) {
		e[Lt] = !0;
	}
	function Gt(e) {
		e[Rt] = void 0;
	}
	var Kt = /* @__PURE__ */ new Set(), qt = {};
	function Jt(e, t) {
		j(e, t), j(e + "Capture", t);
	}
	function j(e, t) {
		for (qt[e] = t, e = 0; e < t.length; e++) Kt.add(t[e]);
	}
	var Yt = RegExp("^[:A-Z_a-z\\u00C0-\\u00D6\\u00D8-\\u00F6\\u00F8-\\u02FF\\u0370-\\u037D\\u037F-\\u1FFF\\u200C-\\u200D\\u2070-\\u218F\\u2C00-\\u2FEF\\u3001-\\uD7FF\\uF900-\\uFDCF\\uFDF0-\\uFFFD][:A-Z_a-z\\u00C0-\\u00D6\\u00D8-\\u00F6\\u00F8-\\u02FF\\u0370-\\u037D\\u037F-\\u1FFF\\u200C-\\u200D\\u2070-\\u218F\\u2C00-\\u2FEF\\u3001-\\uD7FF\\uF900-\\uFDCF\\uFDF0-\\uFFFD\\-.0-9\\u00B7\\u0300-\\u036F\\u203F-\\u2040]*$"), Xt = {}, Zt = {};
	function Qt(e) {
		return Ue.call(Zt, e) ? !0 : Ue.call(Xt, e) ? !1 : Yt.test(e) ? Zt[e] = !0 : (Xt[e] = !0, !1);
	}
	var M = !1;
	function $t() {
		var e = M;
		return M = !1, e;
	}
	function en(e, t, n) {
		if (Qt(t)) {
			if (n === null) e.removeAttribute(t);
			else {
				switch (typeof n) {
					case "undefined":
					case "function":
					case "symbol":
						e.removeAttribute(t);
						return;
					case "boolean":
						var r = t.toLowerCase().slice(0, 5);
						if (r !== "data-" && r !== "aria-") {
							e.removeAttribute(t);
							return;
						}
				}
				e.setAttribute(t, n);
			}
		}
	}
	function tn(e, t, n) {
		if (n === null) e.removeAttribute(t);
		else {
			switch (typeof n) {
				case "undefined":
				case "function":
				case "symbol":
				case "boolean":
					e.removeAttribute(t);
					return;
			}
			e.setAttribute(t, n);
		}
	}
	function nn(e, t, n, r) {
		if (r === null) e.removeAttribute(n);
		else {
			switch (typeof r) {
				case "undefined":
				case "function":
				case "symbol":
				case "boolean":
					e.removeAttribute(n);
					return;
			}
			e.setAttributeNS(t, n, r);
		}
	}
	function rn(e) {
		switch (typeof e) {
			case "bigint":
			case "boolean":
			case "number":
			case "string":
			case "undefined": return e;
			case "object": return e;
			default: return "";
		}
	}
	function an(e) {
		var t = e.type;
		return (e = e.nodeName) && e.toLowerCase() === "input" && (t === "checkbox" || t === "radio");
	}
	function on(e, t, n) {
		var r = Object.getOwnPropertyDescriptor(e.constructor.prototype, t);
		if (!e.hasOwnProperty(t) && r !== void 0 && typeof r.get == "function" && typeof r.set == "function") {
			var i = r.get, a = r.set;
			return Object.defineProperty(e, t, {
				configurable: !0,
				get: function() {
					return i.call(this);
				},
				set: function(e) {
					n = "" + e, a.call(this, e);
				}
			}), Object.defineProperty(e, t, { enumerable: r.enumerable }), {
				getValue: function() {
					return n;
				},
				setValue: function(e) {
					n = "" + e;
				},
				stopTracking: function() {
					e._valueTracker = null, delete e[t];
				}
			};
		}
	}
	function sn(e) {
		if (!e._valueTracker) {
			var t = an(e) ? "checked" : "value";
			e._valueTracker = on(e, t, "" + e[t]);
		}
	}
	function cn(e) {
		if (!e) return !1;
		var t = e._valueTracker;
		if (!t) return !0;
		var n = t.getValue(), r = "";
		return e && (r = an(e) ? e.checked ? "true" : "false" : e.value), e = r, e !== n && (t.setValue(e), !0);
	}
	var ln = /[\n"\\]/g;
	function un(e) {
		return e.replace(ln, function(e) {
			return "\\" + e.charCodeAt(0).toString(16) + " ";
		});
	}
	function dn(e, t, n, r, i, a, o, s) {
		e.name = "", o != null && typeof o != "function" && typeof o != "symbol" && typeof o != "boolean" ? e.type = o : e.removeAttribute("type"), t == null ? o !== "submit" && o !== "reset" || e.removeAttribute("value") : o === "number" ? (t === 0 && e.value === "" || e.value != t) && (e.value = "" + rn(t)) : e.value !== "" + rn(t) && (e.value = "" + rn(t)), t == null ? n == null ? r != null && e.removeAttribute("value") : pn(e, rn(n)) : o === "number" && e.value == t ? pn(e, rn(e.value)) : pn(e, rn(t)), i == null && a != null && (e.defaultChecked = !!a), i != null && (e.checked = i && typeof i != "function" && typeof i != "symbol"), s != null && typeof s != "function" && typeof s != "symbol" && typeof s != "boolean" ? e.name = "" + rn(s) : e.removeAttribute("name");
	}
	function fn(e, t, n, r, i, a, o, s) {
		if (a != null && typeof a != "function" && typeof a != "symbol" && typeof a != "boolean" && (e.type = a), t != null || n != null) {
			if (!(a !== "submit" && a !== "reset" || t != null)) {
				sn(e);
				return;
			}
			n = n == null ? "" : "" + rn(n), t = t == null ? n : "" + rn(t), s || t === e.value || (e.value = t), e.defaultValue = t;
		}
		r ??= i, r = typeof r != "function" && typeof r != "symbol" && !!r, e.checked = s ? e.checked : !!r, e.defaultChecked = !!r, o != null && typeof o != "function" && typeof o != "symbol" && typeof o != "boolean" && (e.name = o), sn(e);
	}
	function pn(e, t) {
		e.defaultValue !== "" + t && (e.defaultValue = "" + t);
	}
	function mn(e, t, n, r) {
		if (e = e.options, t) {
			t = {};
			for (var i = 0; i < n.length; i++) t["$" + n[i]] = !0;
			for (n = 0; n < e.length; n++) i = t.hasOwnProperty("$" + e[n].value), e[n].selected !== i && (e[n].selected = i), i && r && (e[n].defaultSelected = !0);
		} else {
			for (n = "" + rn(n), t = null, i = 0; i < e.length; i++) {
				if (e[i].value === n) {
					e[i].selected = !0, r && (e[i].defaultSelected = !0);
					return;
				}
				t !== null || e[i].disabled || (t = e[i]);
			}
			t !== null && (t.selected = !0);
		}
	}
	function hn(e, t, n) {
		if (t != null && (t = "" + rn(t), t !== e.value && (e.value = t), n == null)) {
			e.defaultValue !== t && (e.defaultValue = t);
			return;
		}
		e.defaultValue = n == null ? "" : "" + rn(n);
	}
	function gn(e, t, n, r) {
		if (t == null) {
			if (r != null) {
				if (n != null) throw Error(s(92));
				if (Se(r)) {
					if (1 < r.length) throw Error(s(93));
					r = r[0];
				}
				n = r;
			}
			n ??= "", t = n;
		}
		n = rn(t), e.defaultValue = n, r = e.textContent, r === n && r !== "" && r !== null && (e.value = r), sn(e);
	}
	function _n(e, t) {
		if (t) {
			var n = e.firstChild;
			if (n && n === e.lastChild && n.nodeType === 3) {
				n.nodeValue = t;
				return;
			}
		}
		e.textContent = t;
	}
	var vn = new Set("animationIterationCount aspectRatio borderImageOutset borderImageSlice borderImageWidth boxFlex boxFlexGroup boxOrdinalGroup columnCount columns flex flexGrow flexPositive flexShrink flexNegative flexOrder gridArea gridRow gridRowEnd gridRowSpan gridRowStart gridColumn gridColumnEnd gridColumnSpan gridColumnStart fontWeight lineClamp lineHeight opacity order orphans scale tabSize widows zIndex zoom fillOpacity floodOpacity stopOpacity strokeDasharray strokeDashoffset strokeMiterlimit strokeOpacity strokeWidth MozAnimationIterationCount MozBoxFlex MozBoxFlexGroup MozLineClamp msAnimationIterationCount msFlex msZoom msFlexGrow msFlexNegative msFlexOrder msFlexPositive msFlexShrink msGridColumn msGridColumnSpan msGridRow msGridRowSpan WebkitAnimationIterationCount WebkitBoxFlex WebKitBoxFlexGroup WebkitBoxOrdinalGroup WebkitColumnCount WebkitColumns WebkitFlex WebkitFlexGrow WebkitFlexPositive WebkitFlexShrink WebkitLineClamp".split(" "));
	function yn(e, t, n) {
		var r = t.indexOf("--") === 0;
		n == null || typeof n == "boolean" || n === "" ? r ? e.setProperty(t, "") : t === "float" ? e.cssFloat = "" : e[t] = "" : r ? e.setProperty(t, n) : typeof n != "number" || n === 0 || vn.has(t) ? t === "float" ? e.cssFloat = n : e[t] = ("" + n).trim() : e[t] = n + "px";
	}
	function bn(e, t, n) {
		if (t != null && typeof t != "object") throw Error(s(62));
		if (e = e.style, n != null) {
			for (var r in n) !n.hasOwnProperty(r) || t != null && t.hasOwnProperty(r) || (r.indexOf("--") === 0 ? e.setProperty(r, "") : r === "float" ? e.cssFloat = "" : e[r] = "", M = !0);
			for (var i in t) r = t[i], t.hasOwnProperty(i) && n[i] !== r && (yn(e, i, r), M = !0);
		} else for (var a in t) t.hasOwnProperty(a) && yn(e, a, t[a]);
	}
	function xn(e) {
		if (e.indexOf("-") === -1) return !1;
		switch (e) {
			case "annotation-xml":
			case "color-profile":
			case "font-face":
			case "font-face-src":
			case "font-face-uri":
			case "font-face-format":
			case "font-face-name":
			case "missing-glyph": return !1;
			default: return !0;
		}
	}
	var Sn = /* @__PURE__ */ new Map([
		["acceptCharset", "accept-charset"],
		["htmlFor", "for"],
		["httpEquiv", "http-equiv"],
		["crossOrigin", "crossorigin"],
		["accentHeight", "accent-height"],
		["alignmentBaseline", "alignment-baseline"],
		["arabicForm", "arabic-form"],
		["baselineShift", "baseline-shift"],
		["capHeight", "cap-height"],
		["clipPath", "clip-path"],
		["clipRule", "clip-rule"],
		["colorInterpolation", "color-interpolation"],
		["colorInterpolationFilters", "color-interpolation-filters"],
		["colorProfile", "color-profile"],
		["colorRendering", "color-rendering"],
		["dominantBaseline", "dominant-baseline"],
		["enableBackground", "enable-background"],
		["fillOpacity", "fill-opacity"],
		["fillRule", "fill-rule"],
		["floodColor", "flood-color"],
		["floodOpacity", "flood-opacity"],
		["fontFamily", "font-family"],
		["fontSize", "font-size"],
		["fontSizeAdjust", "font-size-adjust"],
		["fontStretch", "font-stretch"],
		["fontStyle", "font-style"],
		["fontVariant", "font-variant"],
		["fontWeight", "font-weight"],
		["glyphName", "glyph-name"],
		["glyphOrientationHorizontal", "glyph-orientation-horizontal"],
		["glyphOrientationVertical", "glyph-orientation-vertical"],
		["horizAdvX", "horiz-adv-x"],
		["horizOriginX", "horiz-origin-x"],
		["imageRendering", "image-rendering"],
		["letterSpacing", "letter-spacing"],
		["lightingColor", "lighting-color"],
		["markerEnd", "marker-end"],
		["markerMid", "marker-mid"],
		["markerStart", "marker-start"],
		["maskType", "mask-type"],
		["overlinePosition", "overline-position"],
		["overlineThickness", "overline-thickness"],
		["paintOrder", "paint-order"],
		["panose-1", "panose-1"],
		["pointerEvents", "pointer-events"],
		["renderingIntent", "rendering-intent"],
		["shapeRendering", "shape-rendering"],
		["stopColor", "stop-color"],
		["stopOpacity", "stop-opacity"],
		["strikethroughPosition", "strikethrough-position"],
		["strikethroughThickness", "strikethrough-thickness"],
		["strokeDasharray", "stroke-dasharray"],
		["strokeDashoffset", "stroke-dashoffset"],
		["strokeLinecap", "stroke-linecap"],
		["strokeLinejoin", "stroke-linejoin"],
		["strokeMiterlimit", "stroke-miterlimit"],
		["strokeOpacity", "stroke-opacity"],
		["strokeWidth", "stroke-width"],
		["textAnchor", "text-anchor"],
		["textDecoration", "text-decoration"],
		["textRendering", "text-rendering"],
		["transformOrigin", "transform-origin"],
		["underlinePosition", "underline-position"],
		["underlineThickness", "underline-thickness"],
		["unicodeBidi", "unicode-bidi"],
		["unicodeRange", "unicode-range"],
		["unitsPerEm", "units-per-em"],
		["vAlphabetic", "v-alphabetic"],
		["vHanging", "v-hanging"],
		["vIdeographic", "v-ideographic"],
		["vMathematical", "v-mathematical"],
		["vectorEffect", "vector-effect"],
		["vertAdvY", "vert-adv-y"],
		["vertOriginX", "vert-origin-x"],
		["vertOriginY", "vert-origin-y"],
		["wordSpacing", "word-spacing"],
		["writingMode", "writing-mode"],
		["xmlnsXlink", "xmlns:xlink"],
		["xHeight", "x-height"]
	]), Cn = /^[\u0000-\u001F ]*j[\r\n\t]*a[\r\n\t]*v[\r\n\t]*a[\r\n\t]*s[\r\n\t]*c[\r\n\t]*r[\r\n\t]*i[\r\n\t]*p[\r\n\t]*t[\r\n\t]*:/i;
	function wn(e) {
		return Cn.test("" + e) ? "javascript:throw new Error('React has blocked a javascript: URL as a security precaution.')" : e;
	}
	function Tn() {}
	var En = null;
	function Dn(e) {
		return e = e.target || e.srcElement || window, e.correspondingUseElement && (e = e.correspondingUseElement), e.nodeType === 3 ? e.parentNode : e;
	}
	var On = null, N = null;
	function P(e) {
		var t = Vt(e);
		if (t && (e = t.stateNode)) {
			var n = e[jt] || null;
			a: switch (e = t.stateNode, t.type) {
				case "input":
					if (dn(e, n.value, n.defaultValue, n.defaultValue, n.checked, n.defaultChecked, n.type, n.name), t = n.name, n.type === "radio" && t != null) {
						for (n = e; n.parentNode;) n = n.parentNode;
						for (n = n.querySelectorAll("input[name=\"" + un("" + t) + "\"][type=\"radio\"]"), t = 0; t < n.length; t++) {
							var r = n[t];
							if (r !== e && r.form === e.form) {
								var i = r[jt] || null;
								if (!i) throw Error(s(90));
								dn(r, i.value, i.defaultValue, i.defaultValue, i.checked, i.defaultChecked, i.type, i.name);
							}
						}
						for (t = 0; t < n.length; t++) r = n[t], r.form === e.form && cn(r);
					}
					break a;
				case "textarea":
					hn(e, n.value, n.defaultValue);
					break a;
				case "select": t = n.value, t != null && mn(e, !!n.multiple, t, !1);
			}
		}
	}
	var kn = !1;
	function An(e, t, n) {
		if (kn) return e(t, n);
		kn = !0;
		try {
			return e(t);
		} finally {
			if (kn = !1, (On !== null || N !== null) && (zd(), On && (t = On, e = N, N = On = null, P(t), e))) for (t = 0; t < e.length; t++) P(e[t]);
		}
	}
	function F(e, t) {
		var n = e.stateNode;
		if (n === null) return null;
		var r = n[jt] || null;
		if (r === null) return null;
		n = r[t];
		a: switch (t) {
			case "onClick":
			case "onClickCapture":
			case "onDoubleClick":
			case "onDoubleClickCapture":
			case "onMouseDown":
			case "onMouseDownCapture":
			case "onMouseMove":
			case "onMouseMoveCapture":
			case "onMouseUp":
			case "onMouseUpCapture":
			case "onMouseEnter":
				(r = !r.disabled) || (e = e.type, r = e !== "button" && e !== "input" && e !== "select" && e !== "textarea"), e = !r;
				break a;
			default: e = !1;
		}
		if (e) return null;
		if (n && typeof n != "function") throw Error(s(231, t, typeof n));
		return n;
	}
	var jn = !(typeof window > "u" || window.document === void 0 || window.document.createElement === void 0), Mn = !1;
	if (jn) try {
		var Nn = {};
		Object.defineProperty(Nn, "passive", { get: function() {
			Mn = !0;
		} }), window.addEventListener("test", Nn, Nn), window.removeEventListener("test", Nn, Nn);
	} catch {
		Mn = !1;
	}
	var Pn = null, Fn = null, In = null;
	function Ln() {
		if (In) return In;
		var e, t = Fn, n = t.length, r, i = "value" in Pn ? Pn.value : Pn.textContent, a = i.length;
		for (e = 0; e < n && t[e] === i[e]; e++);
		var o = n - e;
		for (r = 1; r <= o && t[n - r] === i[a - r]; r++);
		return In = i.slice(e, 1 < r ? 1 - r : void 0);
	}
	function Rn(e) {
		var t = e.keyCode;
		return "charCode" in e ? (e = e.charCode, e === 0 && t === 13 && (e = 13)) : e = t, e === 10 && (e = 13), 32 <= e || e === 13 ? e : 0;
	}
	function zn() {
		return !0;
	}
	function Bn() {
		return !1;
	}
	function Vn(e) {
		function t(t, n, r, i, a) {
			for (var o in this._reactName = t, this._targetInst = r, this.type = n, this.nativeEvent = i, this.target = a, this.currentTarget = null, e) e.hasOwnProperty(o) && (t = e[o], this[o] = t ? t(i) : i[o]);
			return this.isDefaultPrevented = (i.defaultPrevented == null ? !1 === i.returnValue : i.defaultPrevented) ? zn : Bn, this.isPropagationStopped = Bn, this;
		}
		return T(t.prototype, {
			preventDefault: function() {
				this.defaultPrevented = !0;
				var e = this.nativeEvent;
				e && (e.preventDefault ? e.preventDefault() : typeof e.returnValue != "unknown" && (e.returnValue = !1), this.isDefaultPrevented = zn);
			},
			stopPropagation: function() {
				var e = this.nativeEvent;
				e && (e.stopPropagation ? e.stopPropagation() : typeof e.cancelBubble != "unknown" && (e.cancelBubble = !0), this.isPropagationStopped = zn);
			},
			persist: function() {},
			isPersistent: zn
		}), t;
	}
	var Hn = {
		eventPhase: 0,
		bubbles: 0,
		cancelable: 0,
		timeStamp: function(e) {
			return e.timeStamp || Date.now();
		},
		defaultPrevented: 0,
		isTrusted: 0
	}, Un = Vn(Hn), Wn = T({}, Hn, {
		view: 0,
		detail: 0
	}), Gn = Vn(Wn), Kn, qn, Jn, Yn = T({}, Wn, {
		screenX: 0,
		screenY: 0,
		clientX: 0,
		clientY: 0,
		pageX: 0,
		pageY: 0,
		ctrlKey: 0,
		shiftKey: 0,
		altKey: 0,
		metaKey: 0,
		getModifierState: or,
		button: 0,
		buttons: 0,
		relatedTarget: function(e) {
			return e.relatedTarget === void 0 ? e.fromElement === e.srcElement ? e.toElement : e.fromElement : e.relatedTarget;
		},
		movementX: function(e) {
			return "movementX" in e ? e.movementX : (e !== Jn && (Jn && e.type === "mousemove" ? (Kn = e.screenX - Jn.screenX, qn = e.screenY - Jn.screenY) : qn = Kn = 0, Jn = e), Kn);
		},
		movementY: function(e) {
			return "movementY" in e ? e.movementY : qn;
		}
	}), Xn = Vn(Yn), Zn = Vn(T({}, Yn, { dataTransfer: 0 })), Qn = Vn(T({}, Wn, { relatedTarget: 0 })), $n = Vn(T({}, Hn, {
		animationName: 0,
		elapsedTime: 0,
		pseudoElement: 0
	})), er = Vn(T({}, Hn, { clipboardData: function(e) {
		return "clipboardData" in e ? e.clipboardData : window.clipboardData;
	} })), tr = Vn(T({}, Hn, { data: 0 })), nr = {
		Esc: "Escape",
		Spacebar: " ",
		Left: "ArrowLeft",
		Up: "ArrowUp",
		Right: "ArrowRight",
		Down: "ArrowDown",
		Del: "Delete",
		Win: "OS",
		Menu: "ContextMenu",
		Apps: "ContextMenu",
		Scroll: "ScrollLock",
		MozPrintableKey: "Unidentified"
	}, rr = {
		8: "Backspace",
		9: "Tab",
		12: "Clear",
		13: "Enter",
		16: "Shift",
		17: "Control",
		18: "Alt",
		19: "Pause",
		20: "CapsLock",
		27: "Escape",
		32: " ",
		33: "PageUp",
		34: "PageDown",
		35: "End",
		36: "Home",
		37: "ArrowLeft",
		38: "ArrowUp",
		39: "ArrowRight",
		40: "ArrowDown",
		45: "Insert",
		46: "Delete",
		112: "F1",
		113: "F2",
		114: "F3",
		115: "F4",
		116: "F5",
		117: "F6",
		118: "F7",
		119: "F8",
		120: "F9",
		121: "F10",
		122: "F11",
		123: "F12",
		144: "NumLock",
		145: "ScrollLock",
		224: "Meta"
	}, ir = {
		Alt: "altKey",
		Control: "ctrlKey",
		Meta: "metaKey",
		Shift: "shiftKey"
	};
	function ar(e) {
		var t = this.nativeEvent;
		return t.getModifierState ? t.getModifierState(e) : (e = ir[e]) ? !!t[e] : !1;
	}
	function or() {
		return ar;
	}
	var sr = Vn(T({}, Wn, {
		key: function(e) {
			if (e.key) {
				var t = nr[e.key] || e.key;
				if (t !== "Unidentified") return t;
			}
			return e.type === "keypress" ? (e = Rn(e), e === 13 ? "Enter" : String.fromCharCode(e)) : e.type === "keydown" || e.type === "keyup" ? rr[e.keyCode] || "Unidentified" : "";
		},
		code: 0,
		location: 0,
		ctrlKey: 0,
		shiftKey: 0,
		altKey: 0,
		metaKey: 0,
		repeat: 0,
		locale: 0,
		getModifierState: or,
		charCode: function(e) {
			return e.type === "keypress" ? Rn(e) : 0;
		},
		keyCode: function(e) {
			return e.type === "keydown" || e.type === "keyup" ? e.keyCode : 0;
		},
		which: function(e) {
			return e.type === "keypress" ? Rn(e) : e.type === "keydown" || e.type === "keyup" ? e.keyCode : 0;
		}
	})), cr = Vn(T({}, Yn, {
		pointerId: 0,
		width: 0,
		height: 0,
		pressure: 0,
		tangentialPressure: 0,
		tiltX: 0,
		tiltY: 0,
		twist: 0,
		pointerType: 0,
		isPrimary: 0
	})), lr = Vn(T({}, Hn, { submitter: 0 })), ur = Vn(T({}, Wn, {
		touches: 0,
		targetTouches: 0,
		changedTouches: 0,
		altKey: 0,
		metaKey: 0,
		ctrlKey: 0,
		shiftKey: 0,
		getModifierState: or
	})), dr = Vn(T({}, Hn, {
		propertyName: 0,
		elapsedTime: 0,
		pseudoElement: 0
	})), fr = Vn(T({}, Yn, {
		deltaX: function(e) {
			return "deltaX" in e ? e.deltaX : "wheelDeltaX" in e ? -e.wheelDeltaX : 0;
		},
		deltaY: function(e) {
			return "deltaY" in e ? e.deltaY : "wheelDeltaY" in e ? -e.wheelDeltaY : "wheelDelta" in e ? -e.wheelDelta : 0;
		},
		deltaZ: 0,
		deltaMode: 0
	})), pr = Vn(T({}, Hn, {
		newState: 0,
		oldState: 0,
		source: 0
	})), mr = [
		9,
		13,
		27,
		32
	], hr = jn && "CompositionEvent" in window, gr = null;
	jn && "documentMode" in document && (gr = document.documentMode);
	var _r = jn && "TextEvent" in window && !gr, vr = jn && (!hr || gr && 8 < gr && 11 >= gr), yr = " ", br = !1;
	function xr(e, t) {
		switch (e) {
			case "keyup": return mr.indexOf(t.keyCode) !== -1;
			case "keydown": return t.keyCode !== 229;
			case "keypress":
			case "mousedown":
			case "focusout": return !0;
			default: return !1;
		}
	}
	function Sr(e) {
		return e = e.detail, typeof e == "object" && "data" in e ? e.data : null;
	}
	var Cr = !1;
	function wr(e, t) {
		switch (e) {
			case "compositionend": return Sr(t);
			case "keypress": return t.which === 32 ? (br = !0, yr) : null;
			case "textInput": return e = t.data, e === yr && br ? null : e;
			default: return null;
		}
	}
	function Tr(e, t) {
		if (Cr) return e === "compositionend" || !hr && xr(e, t) ? (e = Ln(), In = Fn = Pn = null, Cr = !1, e) : null;
		switch (e) {
			case "paste": return null;
			case "keypress":
				if (!(t.ctrlKey || t.altKey || t.metaKey) || t.ctrlKey && t.altKey) {
					if (t.char && 1 < t.char.length) return t.char;
					if (t.which) return String.fromCharCode(t.which);
				}
				return null;
			case "compositionend": return vr && t.locale !== "ko" ? null : t.data;
			default: return null;
		}
	}
	var Er = {
		color: !0,
		date: !0,
		datetime: !0,
		"datetime-local": !0,
		email: !0,
		month: !0,
		number: !0,
		password: !0,
		range: !0,
		search: !0,
		tel: !0,
		text: !0,
		time: !0,
		url: !0,
		week: !0
	};
	function Dr(e) {
		var t = e && e.nodeName && e.nodeName.toLowerCase();
		return t === "input" ? !!Er[e.type] : t === "textarea";
	}
	function Or(e, t, n, r) {
		On ? N ? N.push(r) : N = [r] : On = r, t = Jf(t, "onChange"), 0 < t.length && (n = new Un("onChange", "change", null, n, r), e.push({
			event: n,
			listeners: t
		}));
	}
	var kr = null, Ar = null;
	function jr(e) {
		Vf(e, 0);
	}
	function Mr(e) {
		if (cn(Ht(e))) return e;
	}
	function Nr(e, t) {
		if (e === "change") return t;
	}
	var Pr = !1;
	if (jn) {
		var Fr;
		if (jn) {
			var Ir = "oninput" in document;
			if (!Ir) {
				var Lr = document.createElement("div");
				Lr.setAttribute("oninput", "return;"), Ir = typeof Lr.oninput == "function";
			}
			Fr = Ir;
		} else Fr = !1;
		Pr = Fr && (!document.documentMode || 9 < document.documentMode);
	}
	function Rr() {
		kr && (kr.detachEvent("onpropertychange", zr), Ar = kr = null);
	}
	function zr(e) {
		if (e.propertyName === "value" && Mr(Ar)) {
			var t = [];
			Or(t, Ar, e, Dn(e)), An(jr, t);
		}
	}
	function Br(e, t, n) {
		e === "focusin" ? (Rr(), kr = t, Ar = n, kr.attachEvent("onpropertychange", zr)) : e === "focusout" && Rr();
	}
	function Vr(e) {
		if (e === "selectionchange" || e === "keyup" || e === "keydown") return Mr(Ar);
	}
	function Hr(e, t) {
		if (e === "click") return Mr(t);
	}
	function Ur(e, t) {
		if (e === "input" || e === "change") return Mr(t);
	}
	function Wr(e, t) {
		return e === t && (e !== 0 || 1 / e == 1 / t) || e !== e && t !== t;
	}
	var Gr = typeof Object.is == "function" ? Object.is : Wr;
	function Kr(e, t) {
		if (Gr(e, t)) return !0;
		if (typeof e != "object" || !e || typeof t != "object" || !t) return !1;
		var n = Object.keys(e), r = Object.keys(t);
		if (n.length !== r.length) return !1;
		for (r = 0; r < n.length; r++) {
			var i = n[r];
			if (!Ue.call(t, i) || !Gr(e[i], t[i])) return !1;
		}
		return !0;
	}
	function qr(e) {
		if (e ||= typeof document < "u" ? document : void 0, e === void 0) return null;
		try {
			return e.activeElement || e.body;
		} catch {
			return e.body;
		}
	}
	function Jr(e) {
		for (; e && e.firstChild;) e = e.firstChild;
		return e;
	}
	function Yr(e, t) {
		var n = Jr(e);
		e = 0;
		for (var r; n;) {
			if (n.nodeType === 3) {
				if (r = e + n.textContent.length, e <= t && r >= t) return {
					node: n,
					offset: t - e
				};
				e = r;
			}
			a: {
				for (; n;) {
					if (n.nextSibling) {
						n = n.nextSibling;
						break a;
					}
					n = n.parentNode;
				}
				n = void 0;
			}
			n = Jr(n);
		}
	}
	function Xr(e, t) {
		return e && t ? e === t ? !0 : e && e.nodeType === 3 ? !1 : t && t.nodeType === 3 ? Xr(e, t.parentNode) : "contains" in e ? e.contains(t) : e.compareDocumentPosition ? !!(e.compareDocumentPosition(t) & 16) : !1 : !1;
	}
	function Zr(e) {
		e = e != null && e.ownerDocument != null && e.ownerDocument.defaultView != null ? e.ownerDocument.defaultView : window;
		for (var t = qr(e.document); t instanceof e.HTMLIFrameElement;) {
			try {
				var n = typeof t.contentWindow.location.href == "string";
			} catch {
				n = !1;
			}
			if (n) e = t.contentWindow;
			else break;
			t = qr(e.document);
		}
		return t;
	}
	function Qr(e) {
		var t = e && e.nodeName && e.nodeName.toLowerCase();
		return t && (t === "input" && (e.type === "text" || e.type === "search" || e.type === "tel" || e.type === "url" || e.type === "password") || t === "textarea" || e.contentEditable === "true");
	}
	var $r = jn && "documentMode" in document && 11 >= document.documentMode, ei = null, ti = null, ni = null, ri = !1;
	function ii(e, t, n) {
		var r = n.window === n ? n.document : n.nodeType === 9 ? n : n.ownerDocument;
		ri || ei == null || ei !== qr(r) || (r = ei, "selectionStart" in r && Qr(r) ? r = {
			start: r.selectionStart,
			end: r.selectionEnd
		} : (r = (r.ownerDocument && r.ownerDocument.defaultView || window).getSelection(), r = {
			anchorNode: r.anchorNode,
			anchorOffset: r.anchorOffset,
			focusNode: r.focusNode,
			focusOffset: r.focusOffset
		}), ni && Kr(ni, r) || (ni = r, r = Jf(ti, "onSelect"), 0 < r.length && (t = new Un("onSelect", "select", null, t, n), e.push({
			event: t,
			listeners: r
		}), t.target = ei)));
	}
	function ai(e, t) {
		var n = {};
		return n[e.toLowerCase()] = t.toLowerCase(), n["Webkit" + e] = "webkit" + t, n["Moz" + e] = "moz" + t, n;
	}
	var oi = {
		animationend: ai("Animation", "AnimationEnd"),
		animationiteration: ai("Animation", "AnimationIteration"),
		animationstart: ai("Animation", "AnimationStart"),
		transitionrun: ai("Transition", "TransitionRun"),
		transitionstart: ai("Transition", "TransitionStart"),
		transitioncancel: ai("Transition", "TransitionCancel"),
		transitionend: ai("Transition", "TransitionEnd")
	}, si = {}, ci = {};
	jn && (ci = document.createElement("div").style, "AnimationEvent" in window || (delete oi.animationend.animation, delete oi.animationiteration.animation, delete oi.animationstart.animation), "TransitionEvent" in window || delete oi.transitionend.transition);
	function I(e) {
		if (si[e]) return si[e];
		if (!oi[e]) return e;
		var t = oi[e], n;
		for (n in t) if (t.hasOwnProperty(n) && n in ci) return si[e] = t[n];
		return e;
	}
	var li = I("animationend"), ui = I("animationiteration"), di = I("animationstart"), fi = I("transitionrun"), pi = I("transitionstart"), mi = I("transitioncancel"), hi = I("transitionend"), gi = /* @__PURE__ */ new Map(), _i = "abort auxClick beforeToggle cancel canPlay canPlayThrough click close contextMenu copy cut drag dragEnd dragEnter dragExit dragLeave dragOver dragStart drop durationChange emptied encrypted ended error fullscreenChange fullscreenError gotPointerCapture input invalid keyDown keyPress keyUp load loadedData loadedMetadata loadStart lostPointerCapture mouseDown mouseMove mouseOut mouseOver mouseUp paste pause play playing pointerCancel pointerDown pointerMove pointerOut pointerOver pointerUp progress rateChange reset resize seeked seeking stalled submit suspend timeUpdate touchCancel touchEnd touchStart volumeChange scroll toggle touchMove waiting wheel".split(" ");
	_i.push("scrollEnd");
	function vi(e, t) {
		gi.set(e, t), Jt(t, [e]);
	}
	var yi = 0;
	function bi(e, t) {
		if (e.name != null && e.name !== "auto") return e.name;
		if (t.autoName !== null) return t.autoName;
		e = bd.identifierPrefix;
		var n = yi++;
		return e = "_" + e + "t_" + n.toString(32) + "_", t.autoName = e;
	}
	function xi(e) {
		if (e == null || typeof e == "string") return e;
		var t = null, n = Od;
		if (n !== null) for (var r = 0; r < n.length; r++) {
			var i = e[n[r]];
			if (i != null) {
				if (i === "none") return "none";
				t = t == null ? i : t + (" " + i);
			}
		}
		return t ?? e.default;
	}
	function Si(e, t) {
		return e = xi(e), t = xi(t), t == null ? e === "auto" ? null : e : t === "auto" ? null : t;
	}
	var Ci = typeof reportError == "function" ? reportError : function(e) {
		if (typeof window == "object" && typeof window.ErrorEvent == "function") {
			var t = new window.ErrorEvent("error", {
				bubbles: !0,
				cancelable: !0,
				message: typeof e == "object" && e && typeof e.message == "string" ? String(e.message) : String(e),
				error: e
			});
			if (!window.dispatchEvent(t)) return;
		} else if (typeof process == "object" && typeof process.emit == "function") {
			process.emit("uncaughtException", e);
			return;
		}
		console.error(e);
	}, wi = [], Ti = 0, Ei = 0;
	function Di() {
		for (var e = Ti, t = Ei = Ti = 0; t < e;) {
			var n = wi[t];
			wi[t++] = null;
			var r = wi[t];
			wi[t++] = null;
			var i = wi[t];
			wi[t++] = null;
			var a = wi[t];
			if (wi[t++] = null, r !== null && i !== null) {
				var o = r.pending;
				o === null ? i.next = i : (i.next = o.next, o.next = i), r.pending = i;
			}
			a !== 0 && ji(n, i, a);
		}
	}
	function Oi(e, t, n, r) {
		wi[Ti++] = e, wi[Ti++] = t, wi[Ti++] = n, wi[Ti++] = r, Ei |= r, e.lanes |= r, e = e.alternate, e !== null && (e.lanes |= r);
	}
	function ki(e, t, n, r) {
		return Oi(e, t, n, r), Mi(e);
	}
	function Ai(e, t) {
		return Oi(e, null, null, t), Mi(e);
	}
	function ji(e, t, n) {
		e.lanes |= n;
		var r = e.alternate;
		r !== null && (r.lanes |= n);
		for (var i = !1, a = e.return; a !== null;) a.childLanes |= n, r = a.alternate, r !== null && (r.childLanes |= n), a.tag === 22 && (e = a.stateNode, e === null || e._visibility & 1 || (i = !0)), e = a, a = a.return;
		return e.tag === 3 ? (a = e.stateNode, i && t !== null && (i = 31 - ot(n), e = a.hiddenUpdates, r = e[i], r === null ? e[i] = [t] : r.push(t), t.lane = n | 536870912), a) : null;
	}
	function Mi(e) {
		if (50 < kd) throw kd = 0, Ad = null, Error(s(185));
		for (var t = e.return; t !== null;) e = t, t = e.return;
		return e.tag === 3 ? e.stateNode : null;
	}
	var Ni = {};
	function Pi(e, t, n, r) {
		this.tag = e, this.key = n, this.sibling = this.child = this.return = this.stateNode = this.type = this.elementType = null, this.index = 0, this.refCleanup = this.ref = null, this.pendingProps = t, this.dependencies = this.memoizedState = this.updateQueue = this.memoizedProps = null, this.mode = r, this.subtreeFlags = this.flags = 0, this.deletions = null, this.childLanes = this.lanes = 0, this.alternate = null;
	}
	function Fi(e, t, n, r) {
		return new Pi(e, t, n, r);
	}
	function Ii(e) {
		return e = e.prototype, !(!e || !e.isReactComponent);
	}
	function Li(e, t) {
		var n = e.alternate;
		return n === null ? (n = Fi(e.tag, t, e.key, e.mode), n.elementType = e.elementType, n.type = e.type, n.stateNode = e.stateNode, n.alternate = e, e.alternate = n) : (n.pendingProps = t, n.type = e.type, n.flags = 0, n.subtreeFlags = 0, n.deletions = null), n.flags = e.flags & 1206910976, n.childLanes = e.childLanes, n.lanes = e.lanes, n.child = e.child, n.memoizedProps = e.memoizedProps, n.memoizedState = e.memoizedState, n.updateQueue = e.updateQueue, t = e.dependencies, n.dependencies = t === null ? null : {
			lanes: t.lanes,
			firstContext: t.firstContext
		}, n.sibling = e.sibling, n.index = e.index, n.ref = e.ref, n.refCleanup = e.refCleanup, n;
	}
	function Ri(e, t) {
		e.flags &= 1206910978;
		var n = e.alternate;
		return n === null ? (e.childLanes = 0, e.lanes = t, e.child = null, e.subtreeFlags = 0, e.memoizedProps = null, e.memoizedState = null, e.updateQueue = null, e.dependencies = null, e.stateNode = null) : (e.childLanes = n.childLanes, e.lanes = n.lanes, e.child = n.child, e.subtreeFlags = 0, e.deletions = null, e.memoizedProps = n.memoizedProps, e.memoizedState = n.memoizedState, e.updateQueue = n.updateQueue, e.type = n.type, t = n.dependencies, e.dependencies = t === null ? null : {
			lanes: t.lanes,
			firstContext: t.firstContext
		}), e;
	}
	function L(e, t, n, r, i, a) {
		var o = 0;
		if (r = e, typeof r == "function") Ii(r) && (o = 1);
		else if (typeof r == "string") o = qm(e, n, Oe.current) ? 26 : e === "html" || e === "head" || e === "body" ? 27 : 5;
		else a: switch (r) {
			case pe: return e = Fi(31, n, t, i), e.elementType = pe, e.lanes = a, e;
			case ie: return zi(n.children, i, a, t);
			case D:
				o = 8, i |= 24;
				break;
			case ae: return e = Fi(12, n, t, i | 2), e.elementType = ae, e.lanes = a, e;
			case le: return e = Fi(13, n, t, i), e.elementType = le, e.lanes = a, e;
			case ue: return e = Fi(19, n, t, i), e.elementType = ue, e.lanes = a, e;
			case me:
			case ge: return e = i | 32, e = Fi(30, n, t, e), e.elementType = ge, e.lanes = a, e.stateNode = {
				autoName: null,
				paired: null,
				clones: null,
				ref: null
			}, e;
			default:
				if (typeof r == "object" && r) switch (r.$$typeof) {
					case se:
						o = 10;
						break a;
					case oe:
						o = 9;
						break a;
					case ce:
						o = 11;
						break a;
					case de:
						o = 14;
						break a;
					case fe:
						o = 16, r = null;
						break a;
				}
				o = 29, n = Error(s(130, e === null ? "null" : typeof e, "")), r = null;
		}
		return t = Fi(o, n, t, i), t.elementType = e, t.type = r, t.lanes = a, t;
	}
	function zi(e, t, n, r) {
		return e = Fi(7, e, r, t), e.lanes = n, e;
	}
	function Bi(e, t, n) {
		return e = Fi(6, e, null, t), e.lanes = n, e;
	}
	function Vi(e) {
		var t = Fi(18, null, null, 0);
		return t.stateNode = e, t;
	}
	function Hi(e, t, n) {
		return t = Fi(4, e.children === null ? [] : e.children, e.key, t), t.lanes = n, t.stateNode = {
			containerInfo: e.containerInfo,
			pendingChildren: null,
			implementation: e.implementation
		}, t;
	}
	var Ui = /* @__PURE__ */ new WeakMap();
	function Wi(e, t) {
		if (typeof e == "object" && e) {
			var n = Ui.get(e);
			return n === void 0 ? (t = {
				value: e,
				source: t,
				stack: He(t)
			}, Ui.set(e, t), t) : n;
		}
		return {
			value: e,
			source: t,
			stack: He(t)
		};
	}
	var Gi = [], Ki = 0, qi = null, Ji = 0, Yi = [], Xi = 0, Zi = null, Qi = 1, $i = "";
	function ea(e, t) {
		Gi[Ki++] = Ji, Gi[Ki++] = qi, qi = e, Ji = t;
	}
	function ta(e, t, n) {
		Yi[Xi++] = Qi, Yi[Xi++] = $i, Yi[Xi++] = Zi, Zi = e;
		var r = Qi;
		e = $i;
		var i = 32 - ot(r) - 1;
		r &= ~(1 << i), n += 1;
		var a = 32 - ot(t) + i;
		if (30 < a) {
			var o = i - i % 5;
			a = (r & (1 << o) - 1).toString(32), r >>= o, i -= o, Qi = 1 << 32 - ot(t) + i | n << i | r, $i = a + e;
		} else Qi = 1 << a | n << i | r, $i = e;
	}
	function na(e) {
		e.return !== null && (ea(e, 1), ta(e, 1, 0));
	}
	function ra(e) {
		for (; e === qi;) qi = Gi[--Ki], Gi[Ki] = null, Ji = Gi[--Ki], Gi[Ki] = null;
		for (; e === Zi;) Zi = Yi[--Xi], Yi[Xi] = null, $i = Yi[--Xi], Yi[Xi] = null, Qi = Yi[--Xi], Yi[Xi] = null;
	}
	function ia(e, t) {
		Yi[Xi++] = Qi, Yi[Xi++] = $i, Yi[Xi++] = Zi, Qi = t.id, $i = t.overflow, Zi = e;
	}
	var aa = null, oa = null, R = !1, sa = null, ca = !1, la = Error(s(519));
	function ua(e) {
		throw ga(Wi(Error(s(418, 1 < arguments.length && arguments[1] !== void 0 && arguments[1] ? "text" : "HTML", "")), e)), la;
	}
	function da(e) {
		var t = e.stateNode, n = e.type, r = e.memoizedProps;
		switch (t[At] = e, t[jt] = r, n) {
			case "dialog":
				Q("cancel", t), Q("close", t);
				break;
			case "iframe":
			case "object":
			case "embed":
				Q("load", t);
				break;
			case "video":
			case "audio":
				for (n = 0; n < zf.length; n++) Q(zf[n], t);
				break;
			case "source":
				Q("error", t);
				break;
			case "img":
			case "image":
			case "link":
				Q("error", t), Q("load", t);
				break;
			case "details":
				Q("toggle", t);
				break;
			case "input":
				Q("invalid", t), fn(t, r.value, r.defaultValue, r.checked, r.defaultChecked, r.type, r.name, !0);
				break;
			case "select":
				Q("invalid", t);
				break;
			case "textarea": Q("invalid", t), gn(t, r.value, r.defaultValue, r.children);
		}
		n = r.children, typeof n != "string" && typeof n != "number" && typeof n != "bigint" || t.textContent === "" + n || !0 === r.suppressHydrationWarning || ep(t.textContent, n) ? (r.popover != null && (Q("beforetoggle", t), Q("toggle", t)), r.onScroll != null && Q("scroll", t), r.onScrollEnd != null && Q("scrollend", t), r.onClick != null && (t.onclick = Tn), t = !0) : t = !1, t || ua(e, !0);
	}
	function fa(e) {
		for (aa = e.return; aa;) switch (aa.tag) {
			case 5:
			case 31:
			case 13:
				ca = !1;
				return;
			case 27:
			case 3:
				ca = !0;
				return;
			default: aa = aa.return;
		}
	}
	function pa(e) {
		if (e !== aa) return !1;
		if (!R) return fa(e), R = !0, !1;
		var t = e.tag, n;
		if ((n = t !== 3 && t !== 27) && ((n = t === 5) && (n = e.type, n = n === "form" || n === "button" || pp(e.type, e.memoizedProps)), n = !n), n && oa && ua(e), fa(e), t === 13) {
			if (e = e.memoizedState, e = e === null ? null : e.dehydrated, !e) throw Error(s(317));
			oa = dm(e);
		} else if (t === 31) {
			if (e = e.memoizedState, e = e === null ? null : e.dehydrated, !e) throw Error(s(317));
			oa = dm(e);
		} else t === 27 ? (t = oa, Sp(e.type) ? (e = um, um = null, oa = e) : oa = t) : oa = aa ? lm(e.stateNode.nextSibling) : null;
		return !0;
	}
	function ma() {
		oa = aa = null, R = !1;
	}
	function ha() {
		var e = sa;
		return e !== null && (fd === null ? fd = e : fd.push.apply(fd, e), sa = null), e;
	}
	function ga(e) {
		sa === null ? sa = [e] : sa.push(e);
	}
	var _a = Ee(null), va = null, ya = null;
	function ba(e, t, n) {
		A(_a, t._currentValue), t._currentValue = n;
	}
	function xa(e) {
		e._currentValue = _a.current, De(_a);
	}
	function Sa(e, t, n) {
		for (; e !== null;) {
			var r = e.alternate;
			if ((e.childLanes & t) === t ? r !== null && (r.childLanes & t) !== t && (r.childLanes |= t) : (e.childLanes |= t, r !== null && (r.childLanes |= t)), e === n) break;
			e = e.return;
		}
	}
	function Ca(e, t, n, r) {
		var i = e.child;
		for (i !== null && (i.return = e); i !== null;) {
			var a = i.dependencies;
			if (a !== null) {
				var o = i.child;
				a = a.firstContext;
				a: for (; a !== null;) {
					var c = a;
					a = i;
					for (var l = 0; l < t.length; l++) if (c.context === t[l]) {
						a.lanes |= n, c = a.alternate, c !== null && (c.lanes |= n), Sa(a.return, n, e), r || (o = null);
						break a;
					}
					a = c.next;
				}
			} else if (i.tag === 18) {
				if (o = i.return, o === null) throw Error(s(341));
				o.lanes |= n, a = o.alternate, a !== null && (a.lanes |= n), Sa(o, n, e), o = null;
			} else i.tag === 13 && i.memoizedState !== null && i.memoizedState.dehydrated === null ? (i.lanes |= n, o = i.alternate, o !== null && (o.lanes |= n), Sa(i.return, n, e), o = i.child, o = o === null ? null : o.sibling) : o = i.child;
			if (o !== null) o.return = i;
			else for (o = i; o !== null;) {
				if (o === e) {
					o = null;
					break;
				}
				if (i = o.sibling, i !== null) {
					i.return = o.return, o = i;
					break;
				}
				o = o.return;
			}
			i = o;
		}
	}
	function wa(e, t, n, r) {
		e = null;
		for (var i = t, a = !1; i !== null;) {
			if (!a) {
				if (i.flags & 524288) a = !0;
				else if (i.flags & 262144) break;
			}
			if (i.tag === 10) {
				var o = i.alternate;
				if (o === null) throw Error(s(387));
				if (o = o.memoizedProps, o !== null) {
					var c = i.type;
					Gr(i.pendingProps.value, o.value) || (e === null ? e = [c] : e.push(c));
				}
			} else if (i === je.current) {
				if (o = i.alternate, o === null) throw Error(s(387));
				o.memoizedState.memoizedState !== i.memoizedState.memoizedState && (e === null ? e = [sh] : e.push(sh));
			}
			i = i.return;
		}
		return e !== null && Ca(t, e, n, r), t.flags |= 262144, e !== null;
	}
	function Ta(e) {
		for (e = e.firstContext; e !== null;) {
			if (!Gr(e.context._currentValue, e.memoizedValue)) return !0;
			e = e.next;
		}
		return !1;
	}
	function Ea(e) {
		va = e, ya = null, e = e.dependencies, e !== null && (e.firstContext = null);
	}
	function Da(e) {
		return ka(va, e);
	}
	function Oa(e, t) {
		return va === null && Ea(e), ka(e, t);
	}
	function ka(e, t) {
		var n = t._currentValue;
		if (t = {
			context: t,
			memoizedValue: n,
			next: null
		}, ya === null) {
			if (e === null) throw Error(s(308));
			ya = t, e.dependencies = {
				lanes: 0,
				firstContext: t
			}, e.flags |= 524288;
		} else ya = ya.next = t;
		return n;
	}
	var Aa = typeof AbortController < "u" ? AbortController : function() {
		var e = [], t = this.signal = {
			aborted: !1,
			addEventListener: function(t, n) {
				e.push(n);
			}
		};
		this.abort = function() {
			t.aborted = !0, e.forEach(function(e) {
				return e();
			});
		};
	}, ja = t.unstable_scheduleCallback, Ma = t.unstable_NormalPriority, Na = {
		$$typeof: se,
		Consumer: null,
		Provider: null,
		_currentValue: null,
		_currentValue2: null,
		_threadCount: 0
	};
	function Pa() {
		return {
			controller: new Aa(),
			data: /* @__PURE__ */ new Map(),
			refCount: 0
		};
	}
	function Fa(e) {
		e.refCount--, e.refCount === 0 && ja(Ma, function() {
			e.controller.abort();
		});
	}
	function Ia(e, t) {
		if (e.pendingLanes & 4194048) {
			var n = e.transitionTypes;
			for (n === null && (n = e.transitionTypes = []), e = 0; e < t.length; e++) {
				var r = t[e];
				n.indexOf(r) === -1 && n.push(r);
			}
		}
	}
	var La = null;
	function Ra(e) {
		var t = e.transitionTypes;
		return e.transitionTypes = null, t;
	}
	var za = null, Ba = 0, Va = 0, Ha = null;
	function Ua(e, t) {
		if (za === null) {
			var n = za = [];
			Ba = 0, Va = Pf(), Ha = {
				status: "pending",
				value: void 0,
				then: function(e) {
					n.push(e);
				}
			};
		}
		return Ba++, t.then(Wa, Wa), t;
	}
	function Wa() {
		if (--Ba === 0 && (La = null, za !== null)) {
			Ha !== null && (Ha.status = "fulfilled");
			var e = za;
			za = null, Va = 0, Ha = null;
			for (var t = 0; t < e.length; t++) (0, e[t])();
		}
	}
	function Ga(e, t) {
		var n = [], r = {
			status: "pending",
			value: null,
			reason: null,
			then: function(e) {
				n.push(e);
			}
		};
		return e.then(function() {
			r.status = "fulfilled", r.value = t;
			for (var e = 0; e < n.length; e++) (0, n[e])(t);
		}, function(e) {
			for (r.status = "rejected", r.reason = e, e = 0; e < n.length; e++) (0, n[e])(void 0);
		}), r;
	}
	var Ka = O.S;
	O.S = function(e, t) {
		if (hd = Je(), typeof t == "object" && t && typeof t.then == "function" && Ua(e, t), La !== null) for (var n = bf; n !== null;) Ia(n, La), n = n.next;
		if (n = e.types, n !== null) {
			for (var r = bf; r !== null;) Ia(r, n), r = r.next;
			if (Va !== 0) {
				r = La, r === null && (r = La = []);
				for (var i = 0; i < n.length; i++) {
					var a = n[i];
					r.indexOf(a) === -1 && r.push(a);
				}
			}
		}
		Ka !== null && Ka(e, t);
	};
	var qa = Ee(null);
	function Ja() {
		var e = qa.current;
		return e === null ? K.pooledCache : e;
	}
	function Ya(e, t) {
		t === null ? A(qa, qa.current) : A(qa, t.pool);
	}
	function Xa() {
		var e = Ja();
		return e === null ? null : {
			parent: Na._currentValue,
			pool: e
		};
	}
	var Za = Error(s(460)), Qa = Error(s(474)), $a = Error(s(542)), eo = { then: function() {} };
	function to(e) {
		return e = e.status, e === "fulfilled" || e === "rejected";
	}
	function no(e, t, n) {
		switch (n = e[n], n === void 0 ? e.push(t) : n !== t && (t.then(Tn, Tn), t = n), t.status) {
			case "fulfilled": return t.value;
			case "rejected": throw e = t.reason, oo(e), e === void 0 && !("reason" in t) ? Error(s(600)) : e;
			default:
				if (typeof t.status == "string") t.then(Tn, Tn);
				else {
					if (e = K, e !== null && 100 < e.shellSuspendCounter) throw Error(s(482));
					e = t, e.status = "pending", e.then(function(e) {
						if (t.status === "pending") {
							var n = t;
							n.status = "fulfilled", n.value = e;
						}
					}, function(e) {
						if (t.status === "pending") {
							var n = t;
							n.status = "rejected", n.reason = e;
						}
					});
				}
				switch (t.status) {
					case "fulfilled": return t.value;
					case "rejected": throw e = t.reason, oo(e), e;
				}
				throw io = t, Za;
		}
	}
	function ro(e) {
		try {
			var t = e._init;
			return t(e._payload);
		} catch (e) {
			throw typeof e == "object" && e && typeof e.then == "function" ? (io = e, Za) : e;
		}
	}
	var io = null;
	function ao() {
		if (io === null) throw Error(s(459));
		var e = io;
		return io = null, e;
	}
	function oo(e) {
		if (e === Za || e === $a) throw Error(s(483));
	}
	var so = null, co = 0;
	function lo(e) {
		var t = co;
		return co += 1, so === null && (so = []), no(so, e, t);
	}
	function uo(e, t) {
		t = t.props.ref, e.ref = t === void 0 ? null : t;
	}
	function fo(e, t) {
		throw t.$$typeof === ne ? Error(s(525)) : (e = Object.prototype.toString.call(t), Error(s(31, e === "[object Object]" ? "object with keys {" + Object.keys(t).join(", ") + "}" : e)));
	}
	function po(e) {
		function t(t, n) {
			if (e) {
				var r = t.deletions;
				r === null ? (t.deletions = [n], t.flags |= 16) : r.push(n);
			}
		}
		function n(n, r) {
			if (!e) return null;
			for (; r !== null;) t(n, r), r = r.sibling;
			return null;
		}
		function r(e) {
			for (var t = /* @__PURE__ */ new Map(); e !== null;) e.key === null ? t.set(e.index, e) : t.set(e.key, e), e = e.sibling;
			return t;
		}
		function i(e, t) {
			return e = Li(e, t), e.index = 0, e.sibling = null, e;
		}
		function a(t, n, r) {
			return t.index = r, e ? (r = t.alternate, r === null ? (t.flags |= 134217730, n) : (r = r.index, r < n ? (t.flags |= 2, n) : r)) : (t.flags |= 1048576, n);
		}
		function o(t) {
			return e && t.alternate === null && (t.flags |= 134217730), t;
		}
		function c(e, t, n, r) {
			return t === null || t.tag !== 6 ? (t = Bi(n, e.mode, r), t.return = e, t) : (t = i(t, n), t.return = e, t);
		}
		function l(e, t, n, r) {
			var a = n.type;
			return a === ie ? (e = d(e, t, n.props.children, r, n.key), uo(e, n), e) : t !== null && (t.elementType === a || typeof a == "object" && a && a.$$typeof === fe && ro(a) === t.type) ? (t = i(t, n.props), uo(t, n), t.return = e, t) : (t = L(n.type, n.key, n.props, null, e.mode, r), uo(t, n), t.return = e, t);
		}
		function u(e, t, n, r) {
			return t === null || t.tag !== 4 || t.stateNode.containerInfo !== n.containerInfo || t.stateNode.implementation !== n.implementation ? (t = Hi(n, e.mode, r), t.return = e, t) : (t = i(t, n.children || []), t.return = e, t);
		}
		function d(e, t, n, r, a) {
			return t === null || t.tag !== 7 ? (t = zi(n, e.mode, r, a), t.return = e, t) : (t = i(t, n), t.return = e, t);
		}
		function f(e, t, n) {
			if (typeof t == "string" && t !== "" || typeof t == "number" || typeof t == "bigint") return t = Bi("" + t, e.mode, n), t.return = e, t;
			if (typeof t == "object" && t) {
				switch (t.$$typeof) {
					case E: return n = L(t.type, t.key, t.props, null, e.mode, n), uo(n, t), n.return = e, n;
					case re: return t = Hi(t, e.mode, n), t.return = e, t;
					case fe: return t = ro(t), f(e, t, n);
				}
				if (Se(t) || ye(t)) return t = zi(t, e.mode, n, null), t.return = e, t;
				if (typeof t.then == "function") return f(e, lo(t), n);
				if (t.$$typeof === se) return f(e, Oa(e, t), n);
				fo(e, t);
			}
			return null;
		}
		function p(e, t, n, r) {
			var i = t === null ? null : t.key;
			if (typeof n == "string" && n !== "" || typeof n == "number" || typeof n == "bigint") return i === null ? c(e, t, "" + n, r) : null;
			if (typeof n == "object" && n) {
				switch (n.$$typeof) {
					case E: return n.key === i ? l(e, t, n, r) : null;
					case re: return n.key === i ? u(e, t, n, r) : null;
					case fe: return n = ro(n), p(e, t, n, r);
				}
				if (Se(n) || ye(n)) return i === null ? d(e, t, n, r, null) : null;
				if (typeof n.then == "function") return p(e, t, lo(n), r);
				if (n.$$typeof === se) return p(e, t, Oa(e, n), r);
				fo(e, n);
			}
			return null;
		}
		function m(e, t, n, r, i) {
			if (typeof r == "string" && r !== "" || typeof r == "number" || typeof r == "bigint") return e = e.get(n) || null, c(t, e, "" + r, i);
			if (typeof r == "object" && r) {
				switch (r.$$typeof) {
					case E: return e = e.get(r.key === null ? n : r.key) || null, l(t, e, r, i);
					case re: return e = e.get(r.key === null ? n : r.key) || null, u(t, e, r, i);
					case fe: return r = ro(r), m(e, t, n, r, i);
				}
				if (Se(r) || ye(r)) return e = e.get(n) || null, d(t, e, r, i, null);
				if (typeof r.then == "function") return m(e, t, n, lo(r), i);
				if (r.$$typeof === se) return m(e, t, n, Oa(t, r), i);
				fo(t, r);
			}
			return null;
		}
		function h(i, o, s, c) {
			for (var l = null, u = null, d = o, h = o = 0, g = null; d !== null && h < s.length; h++) {
				d.index > h ? (g = d, d = null) : g = d.sibling;
				var _ = p(i, d, s[h], c);
				if (_ === null) {
					d === null && (d = g);
					break;
				}
				e && d && _.alternate === null && t(i, d), o = a(_, o, h), u === null ? l = _ : u.sibling = _, u = _, d = g;
			}
			if (h === s.length) return n(i, d), R && ea(i, h), l;
			if (d === null) {
				for (; h < s.length; h++) d = f(i, s[h], c), d !== null && (o = a(d, o, h), u === null ? l = d : u.sibling = d, u = d);
				return R && ea(i, h), l;
			}
			for (d = r(d); h < s.length; h++) g = m(d, i, h, s[h], c), g !== null && (e && (_ = g.alternate, _ !== null && d.delete(_.key === null ? h : _.key)), o = a(g, o, h), u === null ? l = g : u.sibling = g, u = g);
			return e && d.forEach(function(e) {
				return t(i, e);
			}), R && ea(i, h), l;
		}
		function g(i, o, c, l) {
			if (c == null) throw Error(s(151));
			for (var u = null, d = null, h = o, g = o = 0, _ = null, v = c.next(); h !== null && !v.done; g++, v = c.next()) {
				h.index > g ? (_ = h, h = null) : _ = h.sibling;
				var y = p(i, h, v.value, l);
				if (y === null) {
					h === null && (h = _);
					break;
				}
				e && h && y.alternate === null && t(i, h), o = a(y, o, g), d === null ? u = y : d.sibling = y, d = y, h = _;
			}
			if (v.done) return n(i, h), R && ea(i, g), u;
			if (h === null) {
				for (; !v.done; g++, v = c.next()) v = f(i, v.value, l), v !== null && (o = a(v, o, g), d === null ? u = v : d.sibling = v, d = v);
				return R && ea(i, g), u;
			}
			for (h = r(h); !v.done; g++, v = c.next()) v = m(h, i, g, v.value, l), v !== null && (e && (_ = v.alternate, _ !== null && h.delete(_.key === null ? g : _.key)), o = a(v, o, g), d === null ? u = v : d.sibling = v, d = v);
			return e && h.forEach(function(e) {
				return t(i, e);
			}), R && ea(i, g), u;
		}
		function _(e, r, a, c) {
			if (typeof a == "object" && a && a.type === ie && a.key === null && a.props.ref === void 0 && (a = a.props.children), typeof a == "object" && a) {
				switch (a.$$typeof) {
					case E:
						a: {
							for (var l = a.key; r !== null;) {
								if (r.key === l) {
									if (l = a.type, l === ie) {
										if (r.tag === 7) {
											n(e, r.sibling), c = i(r, a.props.children), uo(c, a), c.return = e, e = c;
											break a;
										}
									} else if (r.elementType === l || typeof l == "object" && l && l.$$typeof === fe && ro(l) === r.type) {
										n(e, r.sibling), c = i(r, a.props), uo(c, a), c.return = e, e = c;
										break a;
									}
									n(e, r);
									break;
								}
								t(e, r), r = r.sibling;
							}
							a.type === ie ? (c = zi(a.props.children, e.mode, c, a.key), uo(c, a), c.return = e, e = c) : (c = L(a.type, a.key, a.props, null, e.mode, c), uo(c, a), c.return = e, e = c);
						}
						return o(e);
					case re:
						a: {
							for (l = a.key; r !== null;) {
								if (r.key === l) {
									if (r.tag === 4 && r.stateNode.containerInfo === a.containerInfo && r.stateNode.implementation === a.implementation) {
										n(e, r.sibling), c = i(r, a.children || []), c.return = e, e = c;
										break a;
									}
									n(e, r);
									break;
								}
								t(e, r), r = r.sibling;
							}
							c = Hi(a, e.mode, c), c.return = e, e = c;
						}
						return o(e);
					case fe: return a = ro(a), _(e, r, a, c);
				}
				if (Se(a)) return h(e, r, a, c);
				if (ye(a)) {
					if (l = ye(a), typeof l != "function") throw Error(s(150));
					return a = l.call(a), g(e, r, a, c);
				}
				if (typeof a.then == "function") return _(e, r, lo(a), c);
				if (a.$$typeof === se) return _(e, r, Oa(e, a), c);
				fo(e, a);
			}
			return typeof a == "string" && a !== "" || typeof a == "number" || typeof a == "bigint" ? (a = "" + a, r !== null && r.tag === 6 ? (n(e, r.sibling), c = i(r, a), c.return = e, e = c) : (n(e, r), c = Bi(a, e.mode, c), c.return = e, e = c), o(e)) : n(e, r);
		}
		return function(e, t, n, r) {
			try {
				co = 0;
				var i = _(e, t, n, r);
				return so = null, i;
			} catch (t) {
				if (t === Za || t === $a) throw t;
				var a = Fi(29, t, null, e.mode);
				return a.lanes = r, a.return = e, a;
			}
		};
	}
	var mo = po(!0), ho = po(!1), go = !1;
	function _o(e) {
		e.updateQueue = {
			baseState: e.memoizedState,
			firstBaseUpdate: null,
			lastBaseUpdate: null,
			shared: {
				pending: null,
				lanes: 0,
				hiddenCallbacks: null
			},
			callbacks: null
		};
	}
	function vo(e, t) {
		e = e.updateQueue, t.updateQueue === e && (t.updateQueue = {
			baseState: e.baseState,
			firstBaseUpdate: e.firstBaseUpdate,
			lastBaseUpdate: e.lastBaseUpdate,
			shared: e.shared,
			callbacks: null
		});
	}
	function yo(e) {
		return {
			lane: e,
			tag: 0,
			payload: null,
			callback: null,
			next: null
		};
	}
	function bo(e, t, n) {
		var r = e.updateQueue;
		if (r === null) return null;
		if (r = r.shared, G & 2) {
			var i = r.pending;
			return i === null ? t.next = t : (t.next = i.next, i.next = t), r.pending = t, t = Mi(e), ji(e, null, n), t;
		}
		return Oi(e, r, t, n), Mi(e);
	}
	function xo(e, t, n) {
		if (t = t.updateQueue, t !== null && (t = t.shared, n & 4194048)) {
			var r = t.lanes;
			r &= e.pendingLanes, n |= r, t.lanes = n, Ct(e, n);
		}
	}
	function So(e, t) {
		var n = e.updateQueue, r = e.alternate;
		if (r !== null && (r = r.updateQueue, n === r)) {
			var i = null, a = null;
			if (n = n.firstBaseUpdate, n !== null) {
				do {
					var o = {
						lane: n.lane,
						tag: n.tag,
						payload: n.payload,
						callback: null,
						next: null
					};
					a === null ? i = a = o : a = a.next = o, n = n.next;
				} while (n !== null);
				a === null ? i = a = t : a = a.next = t;
			} else i = a = t;
			n = {
				baseState: r.baseState,
				firstBaseUpdate: i,
				lastBaseUpdate: a,
				shared: r.shared,
				callbacks: r.callbacks
			}, e.updateQueue = n;
			return;
		}
		e = n.lastBaseUpdate, e === null ? n.firstBaseUpdate = t : e.next = t, n.lastBaseUpdate = t;
	}
	var Co = !1;
	function wo() {
		if (Co) {
			var e = Ha;
			if (e !== null) throw e;
		}
	}
	function To(e, t, n, r) {
		Co = !1;
		var i = e.updateQueue;
		go = !1;
		var a = i.firstBaseUpdate, o = i.lastBaseUpdate, s = i.shared.pending;
		if (s !== null) {
			i.shared.pending = null;
			var c = s, l = c.next;
			c.next = null, o === null ? a = l : o.next = l, o = c;
			var u = e.alternate;
			u !== null && (u = u.updateQueue, s = u.lastBaseUpdate, s !== o && (s === null ? u.firstBaseUpdate = l : s.next = l, u.lastBaseUpdate = c));
		}
		if (a !== null) {
			var d = i.baseState;
			o = 0, u = l = c = null, s = a;
			do {
				var f = s.lane & -536870913, p = f !== s.lane;
				if (p ? (J & f) === f : (r & f) === f) {
					f !== 0 && f === Va && (Co = !0), u !== null && (u = u.next = {
						lane: 0,
						tag: s.tag,
						payload: s.payload,
						callback: null,
						next: null
					});
					a: {
						var m = e, h = s;
						f = t;
						var g = n;
						switch (h.tag) {
							case 1:
								if (m = h.payload, typeof m == "function") {
									d = m.call(g, d, f);
									break a;
								}
								d = m;
								break a;
							case 3: m.flags = m.flags & -65537 | 128;
							case 0:
								if (m = h.payload, f = typeof m == "function" ? m.call(g, d, f) : m, f == null) break a;
								d = T({}, d, f);
								break a;
							case 2: go = !0;
						}
					}
					f = s.callback, f !== null && (e.flags |= 64, p && (e.flags |= 8192), p = i.callbacks, p === null ? i.callbacks = [f] : p.push(f));
				} else p = {
					lane: f,
					tag: s.tag,
					payload: s.payload,
					callback: s.callback,
					next: null
				}, u === null ? (l = u = p, c = d) : u = u.next = p, o |= f;
				if (s = s.next, s === null) {
					if (s = i.shared.pending, s === null) break;
					p = s, s = p.next, p.next = null, i.lastBaseUpdate = p, i.shared.pending = null;
				}
			} while (1);
			u === null && (c = d), i.baseState = c, i.firstBaseUpdate = l, i.lastBaseUpdate = u, a === null && (i.shared.lanes = 0), od |= o, e.lanes = o, e.memoizedState = d;
		}
	}
	function Eo(e, t) {
		if (typeof e != "function") throw Error(s(191, e));
		e.call(t);
	}
	function Do(e, t) {
		var n = e.callbacks;
		if (n !== null) for (e.callbacks = null, e = 0; e < n.length; e++) Eo(n[e], t);
	}
	var Oo = Ee(null), ko = Ee(0);
	function Ao(e, t) {
		e = ad, A(ko, e), A(Oo, t), ad = e | t.baseLanes;
	}
	function jo() {
		A(ko, ad), A(Oo, Oo.current);
	}
	function Mo() {
		ad = ko.current, De(Oo), De(ko);
	}
	var No = Ee(null), Po = null;
	function Fo(e) {
		var t = e.alternate;
		A(Bo, Bo.current & 1), A(No, e), Po === null && (t === null || Oo.current !== null || t.memoizedState !== null) && (Po = e);
	}
	function Io(e) {
		A(Bo, Bo.current), A(No, e), Po === null && (Po = e);
	}
	function Lo(e) {
		e.tag === 22 ? (A(Bo, Bo.current), A(No, e), Po === null && (Po = e)) : Ro();
	}
	function Ro() {
		A(Bo, Bo.current), A(No, No.current);
	}
	function zo(e) {
		De(No), Po === e && (Po = null), De(Bo);
	}
	var Bo = Ee(0);
	function Vo(e, t) {
		A(No, No.current), A(Bo, t);
	}
	function Ho(e) {
		De(Bo), De(No), Po === e && (Po = null);
	}
	function Uo(e) {
		for (var t = e; t !== null;) {
			if (t.tag === 13) {
				var n = t.memoizedState;
				if (n !== null && (n = n.dehydrated, n === null || om(n) || sm(n))) return t;
			} else if (t.tag === 19 && t.memoizedProps.revealOrder !== "independent") {
				if (t.flags & 128) return t;
			} else if (t.child !== null) {
				t.child.return = t, t = t.child;
				continue;
			}
			if (t === e) break;
			for (; t.sibling === null;) {
				if (t.return === null || t.return === e) return null;
				t = t.return;
			}
			t.sibling.return = t.return, t = t.sibling;
		}
		return null;
	}
	var Wo = 0, z = null, B = null, Go = null, Ko = !1, qo = !1, V = !1, Jo = 0, Yo = 0, Xo = null, Zo = 0;
	function Qo() {
		throw Error(s(321));
	}
	function $o(e, t) {
		if (t === null) return !1;
		for (var n = 0; n < t.length && n < e.length; n++) if (!Gr(e[n], t[n])) return !1;
		return !0;
	}
	function es(e, t, n, r, i, a) {
		return Wo = a, z = t, t.memoizedState = null, t.updateQueue = null, t.lanes = 0, O.H = e === null || e.memoizedState === null ? _c : vc, V = !1, a = n(r, i), V = !1, qo && (a = ns(t, n, r, i)), ts(e), a;
	}
	function ts(e) {
		O.H = gc;
		var t = B !== null && B.next !== null;
		if (Wo = 0, Go = B = z = null, Ko = !1, Yo = 0, Xo = null, t) throw Error(s(300));
		e === null || Fc || (e = e.dependencies, e !== null && Ta(e) && (Fc = !0));
	}
	function ns(e, t, n, r) {
		z = e;
		var i = 0;
		do {
			if (qo && (Xo = null), Yo = 0, qo = !1, 25 <= i) throw Error(s(301));
			if (i += 1, Go = B = null, e.updateQueue != null) {
				var a = e.updateQueue;
				a.lastEffect = null, a.events = null, a.stores = null, a.memoCache != null && (a.memoCache.index = 0);
			}
			O.H = yc, a = t(n, r);
		} while (qo);
		return a;
	}
	function rs() {
		var e = O.H, t = e.useState()[0];
		return t = typeof t.then == "function" ? us(t) : t, e = e.useState()[0], (B === null ? null : B.memoizedState) !== e && (z.flags |= 1024), t;
	}
	function is() {
		var e = Jo !== 0;
		return Jo = 0, e;
	}
	function as(e, t, n) {
		t.updateQueue = e.updateQueue, t.flags &= -2053, e.lanes &= ~n;
	}
	function os(e) {
		if (Ko) {
			for (e = e.memoizedState; e !== null;) {
				var t = e.queue;
				t !== null && (t.pending = null), e = e.next;
			}
			Ko = !1;
		}
		Wo = 0, Go = B = z = null, qo = !1, Yo = Jo = 0, Xo = null;
	}
	function ss() {
		var e = {
			memoizedState: null,
			baseState: null,
			baseQueue: null,
			queue: null,
			next: null
		};
		return Go === null ? z.memoizedState = Go = e : Go = Go.next = e, Go;
	}
	function cs() {
		if (B === null) {
			var e = z.alternate;
			e = e === null ? null : e.memoizedState;
		} else e = B.next;
		var t = Go === null ? z.memoizedState : Go.next;
		if (t !== null) Go = t, B = e;
		else {
			if (e === null) throw z.alternate === null ? Error(s(467)) : Error(s(310));
			B = e, e = {
				memoizedState: B.memoizedState,
				baseState: B.baseState,
				baseQueue: B.baseQueue,
				queue: B.queue,
				next: null
			}, Go === null ? z.memoizedState = Go = e : Go = Go.next = e;
		}
		return Go;
	}
	function ls() {
		return {
			lastEffect: null,
			events: null,
			stores: null,
			memoCache: null
		};
	}
	function us(e) {
		var t = Yo;
		return Yo += 1, Xo === null && (Xo = []), e = no(Xo, e, t), t = z, (Go === null ? t.memoizedState : Go.next) === null && (t = t.alternate, O.H = t === null || t.memoizedState === null ? _c : vc), e;
	}
	function ds(e) {
		if (typeof e == "object" && e) {
			if (typeof e.then == "function") return us(e);
			if (e.$$typeof === _e) return;
			if (e.$$typeof === se) return Da(e);
		}
		throw Error(s(438, String(e)));
	}
	function fs(e) {
		var t = null, n = z.updateQueue;
		if (n !== null && (t = n.memoCache), t == null) {
			var r = z.alternate;
			r !== null && (r = r.updateQueue, r !== null && (r = r.memoCache, r != null && (t = {
				data: r.data.map(function(e) {
					return e.slice();
				}),
				index: 0
			})));
		}
		if (t ??= {
			data: [],
			index: 0
		}, n === null && (n = ls(), z.updateQueue = n), n.memoCache = t, n = t.data[t.index], n === void 0) for (n = t.data[t.index] = Array(e), r = 0; r < e; r++) n[r] = he;
		return t.index++, n;
	}
	function ps(e, t) {
		return typeof t == "function" ? t(e) : t;
	}
	function ms(e) {
		return hs(cs(), B, e);
	}
	function hs(e, t, n) {
		var r = e.queue;
		if (r === null) throw Error(s(311));
		r.lastRenderedReducer = n;
		var i = e.baseQueue, a = r.pending;
		if (a !== null) {
			if (i !== null) {
				var o = i.next;
				i.next = a.next, a.next = o;
			}
			t.baseQueue = i = a, r.pending = null;
		}
		if (a = e.baseState, i === null) e.memoizedState = a;
		else {
			t = i.next;
			var c = o = null, l = null, u = t, d = !1;
			do {
				var f = u.lane & -536870913;
				if (f === u.lane ? (Wo & f) === f : (J & f) === f) {
					var p = u.revertLane;
					if (p === 0) l !== null && (l = l.next = {
						lane: 0,
						revertLane: 0,
						gesture: null,
						action: u.action,
						hasEagerState: u.hasEagerState,
						eagerState: u.eagerState,
						next: null
					}), f === Va && (d = !0);
					else if ((Wo & p) === p) {
						u = u.next, p === Va && (d = !0);
						continue;
					} else f = {
						lane: 0,
						revertLane: u.revertLane,
						gesture: null,
						action: u.action,
						hasEagerState: u.hasEagerState,
						eagerState: u.eagerState,
						next: null
					}, l === null ? (c = l = f, o = a) : l = l.next = f, z.lanes |= p, od |= p;
					f = u.action, V && n(a, f), a = u.hasEagerState ? u.eagerState : n(a, f);
				} else p = {
					lane: f,
					revertLane: u.revertLane,
					gesture: u.gesture,
					action: u.action,
					hasEagerState: u.hasEagerState,
					eagerState: u.eagerState,
					next: null
				}, l === null ? (c = l = p, o = a) : l = l.next = p, z.lanes |= f, od |= f;
				u = u.next;
			} while (u !== null && u !== t);
			if (l === null ? o = a : l.next = c, !Gr(a, e.memoizedState) && (Fc = !0, d && (n = Ha, n !== null))) throw n;
			e.memoizedState = a, e.baseState = o, e.baseQueue = l, r.lastRenderedState = a;
		}
		return i === null && (r.lanes = 0), [e.memoizedState, r.dispatch];
	}
	function gs(e) {
		var t = cs(), n = t.queue;
		if (n === null) throw Error(s(311));
		n.lastRenderedReducer = e;
		var r = n.dispatch, i = n.pending, a = t.memoizedState;
		if (i !== null) {
			n.pending = null;
			var o = i = i.next;
			do
				a = e(a, o.action), o = o.next;
			while (o !== i);
			Gr(a, t.memoizedState) || (Fc = !0), t.memoizedState = a, t.baseQueue === null && (t.baseState = a), n.lastRenderedState = a;
		}
		return [a, r];
	}
	function _s(e, t, n) {
		var r = z, i = cs(), a = R;
		if (a) {
			if (n === void 0) throw Error(s(407));
			n = n();
		} else n = t();
		var o = !Gr((B || i).memoizedState, n);
		if (o && (i.memoizedState = n, Fc = !0), i = i.queue, Hs(bs.bind(null, r, i, e), [e]), e = i.getSnapshot !== t || o || Go !== null && !!(Go.memoizedState.tag & 1), Ls(e ? 9 : 8, { destroy: void 0 }, ys.bind(null, r, i, n, t), null), e) {
			if (r.flags |= 2048, K === null) throw Error(s(349));
			a || Wo & 127 || vs(r, t, n);
		}
		return n;
	}
	function vs(e, t, n) {
		e.flags |= 16384, e = {
			getSnapshot: t,
			value: n
		}, t = z.updateQueue, t === null ? (t = ls(), z.updateQueue = t, t.stores = [e]) : (n = t.stores, n === null ? t.stores = [e] : n.push(e));
	}
	function ys(e, t, n, r) {
		t.value = n, t.getSnapshot = r, xs(t) && Ss(e);
	}
	function bs(e, t, n) {
		return n(function() {
			xs(t) && Ss(e);
		});
	}
	function xs(e) {
		var t = e.getSnapshot;
		e = e.value;
		try {
			var n = t();
			return !Gr(e, n);
		} catch {
			return !0;
		}
	}
	function Ss(e) {
		var t = Ai(e, 2);
		t !== null && Pd(t, e, 2);
	}
	function Cs(e) {
		var t = ss();
		if (typeof e == "function") {
			var n = e;
			if (e = n(), V) {
				at(!0);
				try {
					n();
				} finally {
					at(!1);
				}
			}
		}
		return t.memoizedState = t.baseState = e, t.queue = {
			pending: null,
			lanes: 0,
			dispatch: null,
			lastRenderedReducer: ps,
			lastRenderedState: e
		}, t;
	}
	function ws(e, t, n, r) {
		return e.baseState = n, hs(e, B, typeof r == "function" ? r : ps);
	}
	function Ts(e, t, n, r, i) {
		if (pc(e)) throw Error(s(485));
		if (e = t.action, e !== null) {
			var a = {
				payload: i,
				action: e,
				next: null,
				isTransition: !0,
				status: "pending",
				value: null,
				reason: null,
				listeners: [],
				then: function(e) {
					a.listeners.push(e);
				}
			};
			O.T === null ? a.isTransition = !1 : n(!0), r(a), n = t.pending, n === null ? (a.next = t.pending = a, Es(t, a)) : (a.next = n.next, t.pending = n.next = a);
		}
	}
	function Es(e, t) {
		var n = t.action, r = t.payload, i = e.state;
		if (t.isTransition) {
			var a = O.T, o = {};
			o.types = a === null ? null : a.types, O.T = o;
			try {
				var s = n(i, r), c = O.S;
				c !== null && c(o, s), Ds(e, t, s);
			} catch (n) {
				ks(e, t, n);
			} finally {
				a !== null && o.types !== null && (a.types = o.types), O.T = a;
			}
		} else try {
			a = n(i, r), Ds(e, t, a);
		} catch (n) {
			ks(e, t, n);
		}
	}
	function Ds(e, t, n) {
		typeof n == "object" && n && typeof n.then == "function" ? n.then(function(n) {
			Os(e, t, n);
		}, function(n) {
			return ks(e, t, n);
		}) : Os(e, t, n);
	}
	function Os(e, t, n) {
		t.status = "fulfilled", t.value = n, As(t), e.state = n, t = e.pending, t !== null && (n = t.next, n === t ? e.pending = null : (n = n.next, t.next = n, Es(e, n)));
	}
	function ks(e, t, n) {
		var r = e.pending;
		if (e.pending = null, r !== null) {
			r = r.next;
			do
				t.status = "rejected", t.reason = n, As(t), t = t.next;
			while (t !== r);
		}
		e.action = null;
	}
	function As(e) {
		e = e.listeners;
		for (var t = 0; t < e.length; t++) (0, e[t])();
	}
	function js(e, t) {
		return t;
	}
	function Ms(e, t) {
		if (R) {
			var n = K.formState;
			if (n !== null) {
				a: {
					var r = z;
					if (R) {
						if (oa) {
							b: {
								for (var i = oa, a = ca; i.nodeType !== 8;) {
									if (!a) {
										i = null;
										break b;
									}
									if (i = lm(i.nextSibling), i === null) {
										i = null;
										break b;
									}
								}
								a = i.data, i = a === "F!" || a === "F" ? i : null;
							}
							if (i) {
								oa = lm(i.nextSibling), r = i.data === "F!";
								break a;
							}
						}
						ua(r);
					}
					r = !1;
				}
				r && (t = n[0]);
			}
		}
		return n = ss(), n.memoizedState = n.baseState = t, r = {
			pending: null,
			lanes: 0,
			dispatch: null,
			lastRenderedReducer: js,
			lastRenderedState: t
		}, n.queue = r, n = uc.bind(null, z, r), r.dispatch = n, r = Cs(!1), a = fc.bind(null, z, !1, r.queue), r = ss(), i = {
			state: t,
			dispatch: null,
			action: e,
			pending: null
		}, r.queue = i, n = Ts.bind(null, z, i, a, n), i.dispatch = n, r.memoizedState = e, [
			t,
			n,
			!1
		];
	}
	function Ns(e) {
		return Ps(cs(), B, e);
	}
	function Ps(e, t, n) {
		if (t = hs(e, t, js)[0], e = ms(ps)[0], typeof t == "object" && t && typeof t.then == "function") try {
			var r = us(t);
		} catch (e) {
			throw e === Za ? $a : e;
		}
		else r = t;
		t = cs();
		var i = t.queue, a = i.dispatch;
		return n !== t.memoizedState && (z.flags |= 2048, Ls(9, { destroy: void 0 }, Fs.bind(null, i, n), null)), [
			r,
			a,
			e
		];
	}
	function Fs(e, t) {
		e.action = t;
	}
	function Is(e) {
		var t = cs(), n = B;
		if (n !== null) return Ps(t, n, e);
		cs(), t = t.memoizedState, n = cs();
		var r = n.queue.dispatch;
		return n.memoizedState = e, [
			t,
			r,
			!1
		];
	}
	function Ls(e, t, n, r) {
		return e = {
			tag: e,
			create: n,
			deps: r,
			inst: t,
			next: null
		}, t = z.updateQueue, t === null && (t = ls(), z.updateQueue = t), n = t.lastEffect, n === null ? t.lastEffect = e.next = e : (r = n.next, n.next = e, e.next = r, t.lastEffect = e), e;
	}
	function Rs() {
		return cs().memoizedState;
	}
	function zs(e, t, n, r) {
		var i = ss();
		z.flags |= e, i.memoizedState = Ls(1 | t, { destroy: void 0 }, n, r === void 0 ? null : r);
	}
	function Bs(e, t, n, r) {
		var i = cs();
		r = r === void 0 ? null : r;
		var a = i.memoizedState.inst;
		B !== null && r !== null && $o(r, B.memoizedState.deps) ? i.memoizedState = Ls(t, a, n, r) : (z.flags |= e, i.memoizedState = Ls(1 | t, a, n, r));
	}
	function Vs(e, t) {
		zs(8390656, 8, e, t);
	}
	function Hs(e, t) {
		Bs(2048, 8, e, t);
	}
	function Us(e) {
		z.flags |= 4;
		var t = z.updateQueue;
		if (t === null) t = ls(), z.updateQueue = t, t.events = [e];
		else {
			var n = t.events;
			n === null ? t.events = [e] : n.push(e);
		}
	}
	function Ws(e) {
		var t = cs().memoizedState;
		return Us({
			ref: t,
			nextImpl: e
		}), function() {
			if (G & 2) throw Error(s(440));
			return t.impl.apply(void 0, arguments);
		};
	}
	function Gs(e, t) {
		return Bs(4, 2, e, t);
	}
	function Ks(e, t) {
		return Bs(4, 4, e, t);
	}
	function qs(e, t) {
		if (typeof t == "function") {
			e = e();
			var n = t(e);
			return function() {
				typeof n == "function" ? n() : t(null);
			};
		}
		if (t != null) return e = e(), t.current = e, function() {
			t.current = null;
		};
	}
	function Js(e, t, n) {
		n = n == null ? null : n.concat([e]), Bs(4, 4, qs.bind(null, t, e), n);
	}
	function Ys() {}
	function Xs(e, t) {
		var n = cs();
		t = t === void 0 ? null : t;
		var r = n.memoizedState;
		return t !== null && $o(t, r[1]) ? r[0] : (n.memoizedState = [e, t], e);
	}
	function Zs(e, t) {
		var n = cs();
		t = t === void 0 ? null : t;
		var r = n.memoizedState;
		if (t !== null && $o(t, r[1])) return r[0];
		if (r = e(), V) {
			at(!0);
			try {
				e();
			} finally {
				at(!1);
			}
		}
		return n.memoizedState = [r, t], r;
	}
	function Qs(e, t, n) {
		return n === void 0 || Wo & 1073741824 && !(J & 261930) ? e.memoizedState = t : (e.memoizedState = n, e = Md(), z.lanes |= e, od |= e, n);
	}
	function $s(e, t, n, r) {
		return Gr(n, t) ? n : Oo.current === null ? !(Wo & 106) || Wo & 1073741824 && !(J & 261930) ? (Fc = !0, e.memoizedState = n) : (e = Md(), z.lanes |= e, od |= e, t) : (e = Qs(e, n, r), Gr(e, t) || (Fc = !0), e);
	}
	function ec(e, t, n, r, i) {
		var a = k.p;
		k.p = a !== 0 && 8 > a ? a : 8;
		var o = O.T, s = {};
		s.types = o === null ? null : o.types, O.T = s, fc(e, !1, t, n);
		try {
			var c = i(), l = O.S;
			l !== null && l(s, c), typeof c == "object" && c && typeof c.then == "function" ? dc(e, t, Ga(c, r), jd(e)) : dc(e, t, r, jd(e));
		} catch (n) {
			dc(e, t, {
				then: function() {},
				status: "rejected",
				reason: n
			}, jd());
		} finally {
			k.p = a, o !== null && s.types !== null && (o.types = s.types), O.T = o;
		}
	}
	function tc() {}
	function nc(e, t, n, r) {
		if (e.tag !== 5) throw Error(s(476));
		var i = rc(e).queue;
		ec(e, i, t, Ce, n === null ? tc : function() {
			return ic(e), n(r);
		});
	}
	function rc(e) {
		var t = e.memoizedState;
		if (t !== null) return t;
		t = {
			memoizedState: Ce,
			baseState: Ce,
			baseQueue: null,
			queue: {
				pending: null,
				lanes: 0,
				dispatch: null,
				lastRenderedReducer: ps,
				lastRenderedState: Ce
			},
			next: null
		};
		var n = {};
		return t.next = {
			memoizedState: n,
			baseState: n,
			baseQueue: null,
			queue: {
				pending: null,
				lanes: 0,
				dispatch: null,
				lastRenderedReducer: ps,
				lastRenderedState: n
			},
			next: null
		}, e.memoizedState = t, e = e.alternate, e !== null && (e.memoizedState = t), t;
	}
	function ic(e) {
		var t = rc(e);
		t.next === null && (t = e.alternate.memoizedState), dc(e, t.next.queue, {}, jd());
	}
	function ac() {
		return Da(sh);
	}
	function oc() {
		return cs().memoizedState;
	}
	function sc() {
		return cs().memoizedState;
	}
	function cc(e) {
		for (var t = e.return; t !== null;) {
			switch (t.tag) {
				case 24:
				case 3:
					var n = jd();
					e = yo(n);
					var r = bo(t, e, n);
					r !== null && (Pd(r, t, n), xo(r, t, n)), t = { cache: Pa() }, e.payload = t;
					return;
			}
			t = t.return;
		}
	}
	function lc(e, t, n) {
		var r = jd();
		n = {
			lane: r,
			revertLane: 0,
			gesture: null,
			action: n,
			hasEagerState: !1,
			eagerState: null,
			next: null
		}, pc(e) ? mc(t, n) : (n = ki(e, t, n, r), n !== null && (Pd(n, e, r), hc(n, t, r)));
	}
	function uc(e, t, n) {
		dc(e, t, n, jd());
	}
	function dc(e, t, n, r) {
		var i = {
			lane: r,
			revertLane: 0,
			gesture: null,
			action: n,
			hasEagerState: !1,
			eagerState: null,
			next: null
		};
		if (pc(e)) mc(t, i);
		else {
			var a = e.alternate;
			if (e.lanes === 0 && (a === null || a.lanes === 0) && (a = t.lastRenderedReducer, a !== null)) try {
				var o = t.lastRenderedState, s = a(o, n);
				if (i.hasEagerState = !0, i.eagerState = s, Gr(s, o)) return Oi(e, t, i, 0), K === null && Di(), !1;
			} catch {}
			if (n = ki(e, t, i, r), n !== null) return Pd(n, e, r), hc(n, t, r), !0;
		}
		return !1;
	}
	function fc(e, t, n, r) {
		if (r = {
			lane: 2,
			revertLane: Pf(),
			gesture: null,
			action: r,
			hasEagerState: !1,
			eagerState: null,
			next: null
		}, pc(e)) {
			if (t) throw Error(s(479));
		} else t = ki(e, n, r, 2), t !== null && Pd(t, e, 2);
	}
	function pc(e) {
		var t = e.alternate;
		return e === z || t !== null && t === z;
	}
	function mc(e, t) {
		qo = Ko = !0;
		var n = e.pending;
		n === null ? t.next = t : (t.next = n.next, n.next = t), e.pending = t;
	}
	function hc(e, t, n) {
		if (n & 4194048) {
			var r = t.lanes;
			r &= e.pendingLanes, n |= r, t.lanes = n, Ct(e, n);
		}
	}
	var gc = {
		readContext: Da,
		use: ds,
		useCallback: Qo,
		useContext: Qo,
		useEffect: Qo,
		useImperativeHandle: Qo,
		useLayoutEffect: Qo,
		useInsertionEffect: Qo,
		useMemo: Qo,
		useReducer: Qo,
		useRef: Qo,
		useState: Qo,
		useDebugValue: Qo,
		useDeferredValue: Qo,
		useTransition: Qo,
		useSyncExternalStore: Qo,
		useId: Qo,
		useHostTransitionStatus: Qo,
		useFormState: Qo,
		useActionState: Qo,
		useOptimistic: Qo,
		useMemoCache: Qo,
		useCacheRefresh: Qo,
		useEffectEvent: Qo
	}, _c = {
		readContext: Da,
		use: ds,
		useCallback: function(e, t) {
			return ss().memoizedState = [e, t === void 0 ? null : t], e;
		},
		useContext: Da,
		useEffect: Vs,
		useImperativeHandle: function(e, t, n) {
			n = n == null ? null : n.concat([e]), zs(4194308, 4, qs.bind(null, t, e), n);
		},
		useLayoutEffect: function(e, t) {
			return zs(4194308, 4, e, t);
		},
		useInsertionEffect: function(e, t) {
			zs(4, 2, e, t);
		},
		useMemo: function(e, t) {
			var n = ss();
			t = t === void 0 ? null : t;
			var r = e();
			if (V) {
				at(!0);
				try {
					e();
				} finally {
					at(!1);
				}
			}
			return n.memoizedState = [r, t], r;
		},
		useReducer: function(e, t, n) {
			var r = ss();
			if (n !== void 0) {
				var i = n(t);
				if (V) {
					at(!0);
					try {
						n(t);
					} finally {
						at(!1);
					}
				}
			} else i = t;
			return r.memoizedState = r.baseState = i, e = {
				pending: null,
				lanes: 0,
				dispatch: null,
				lastRenderedReducer: e,
				lastRenderedState: i
			}, r.queue = e, e = e.dispatch = lc.bind(null, z, e), [r.memoizedState, e];
		},
		useRef: function(e) {
			var t = ss();
			return e = { current: e }, t.memoizedState = e;
		},
		useState: function(e) {
			e = Cs(e);
			var t = e.queue, n = uc.bind(null, z, t);
			return t.dispatch = n, [e.memoizedState, n];
		},
		useDebugValue: Ys,
		useDeferredValue: function(e, t) {
			return Qs(ss(), e, t);
		},
		useTransition: function() {
			var e = Cs(!1);
			return e = ec.bind(null, z, e.queue, !0, !1), ss().memoizedState = e, [!1, e];
		},
		useSyncExternalStore: function(e, t, n) {
			var r = z, i = ss();
			if (R) {
				if (n === void 0) throw Error(s(407));
				n = n();
			} else {
				if (n = t(), K === null) throw Error(s(349));
				J & 127 || vs(r, t, n);
			}
			i.memoizedState = n;
			var a = {
				value: n,
				getSnapshot: t
			};
			return i.queue = a, Vs(bs.bind(null, r, a, e), [e]), r.flags |= 2048, Ls(9, { destroy: void 0 }, ys.bind(null, r, a, n, t), null), n;
		},
		useId: function() {
			var e = ss(), t = K.identifierPrefix;
			if (R) {
				var n = $i, r = Qi;
				n = (r & ~(1 << 32 - ot(r) - 1)).toString(32) + n, t = "_" + t + "R_" + n, n = Jo++, 0 < n && (t += "H" + n.toString(32)), t += "_";
			} else n = Zo++, t = "_" + t + "r_" + n.toString(32) + "_";
			return e.memoizedState = t;
		},
		useHostTransitionStatus: ac,
		useFormState: Ms,
		useActionState: Ms,
		useOptimistic: function(e) {
			var t = ss();
			t.memoizedState = t.baseState = e;
			var n = {
				pending: null,
				lanes: 0,
				dispatch: null,
				lastRenderedReducer: null,
				lastRenderedState: null
			};
			return t.queue = n, t = fc.bind(null, z, !0, n), n.dispatch = t, [e, t];
		},
		useMemoCache: fs,
		useCacheRefresh: function() {
			return ss().memoizedState = cc.bind(null, z);
		},
		useEffectEvent: function(e) {
			var t = ss(), n = { impl: e };
			return t.memoizedState = n, function() {
				if (G & 2) throw Error(s(440));
				return n.impl.apply(void 0, arguments);
			};
		}
	}, vc = {
		readContext: Da,
		use: ds,
		useCallback: Xs,
		useContext: Da,
		useEffect: Hs,
		useImperativeHandle: Js,
		useInsertionEffect: Gs,
		useLayoutEffect: Ks,
		useMemo: Zs,
		useReducer: ms,
		useRef: Rs,
		useState: function() {
			return ms(ps);
		},
		useDebugValue: Ys,
		useDeferredValue: function(e, t) {
			return $s(cs(), B.memoizedState, e, t);
		},
		useTransition: function() {
			var e = ms(ps)[0], t = cs().memoizedState;
			return [typeof e == "boolean" ? e : us(e), t];
		},
		useSyncExternalStore: _s,
		useId: oc,
		useHostTransitionStatus: ac,
		useFormState: Ns,
		useActionState: Ns,
		useOptimistic: function(e, t) {
			return ws(cs(), B, e, t);
		},
		useMemoCache: fs,
		useCacheRefresh: sc,
		useEffectEvent: Ws
	}, yc = {
		readContext: Da,
		use: ds,
		useCallback: Xs,
		useContext: Da,
		useEffect: Hs,
		useImperativeHandle: Js,
		useInsertionEffect: Gs,
		useLayoutEffect: Ks,
		useMemo: Zs,
		useReducer: gs,
		useRef: Rs,
		useState: function() {
			return gs(ps);
		},
		useDebugValue: Ys,
		useDeferredValue: function(e, t) {
			var n = cs();
			return B === null ? Qs(n, e, t) : $s(n, B.memoizedState, e, t);
		},
		useTransition: function() {
			var e = gs(ps)[0], t = cs().memoizedState;
			return [typeof e == "boolean" ? e : us(e), t];
		},
		useSyncExternalStore: _s,
		useId: oc,
		useHostTransitionStatus: ac,
		useFormState: Is,
		useActionState: Is,
		useOptimistic: function(e, t) {
			var n = cs();
			return B === null ? (n.baseState = e, [e, n.queue.dispatch]) : ws(n, B, e, t);
		},
		useMemoCache: fs,
		useCacheRefresh: sc,
		useEffectEvent: Ws
	};
	function bc(e, t, n, r) {
		t = e.memoizedState, n = n(r, t), n = n == null ? t : T({}, t, n), e.memoizedState = n, e.lanes === 0 && (e.updateQueue.baseState = n);
	}
	var xc = {
		enqueueSetState: function(e, t, n) {
			e = e._reactInternals;
			var r = jd(), i = yo(r);
			i.payload = t, n != null && (i.callback = n), t = bo(e, i, r), t !== null && (Pd(t, e, r), xo(t, e, r));
		},
		enqueueReplaceState: function(e, t, n) {
			e = e._reactInternals;
			var r = jd(), i = yo(r);
			i.tag = 1, i.payload = t, n != null && (i.callback = n), t = bo(e, i, r), t !== null && (Pd(t, e, r), xo(t, e, r));
		},
		enqueueForceUpdate: function(e, t) {
			e = e._reactInternals;
			var n = jd(), r = yo(n);
			r.tag = 2, t != null && (r.callback = t), t = bo(e, r, n), t !== null && (Pd(t, e, n), xo(t, e, n));
		}
	};
	function Sc(e, t, n, r, i, a, o) {
		return e = e.stateNode, typeof e.shouldComponentUpdate == "function" ? e.shouldComponentUpdate(r, a, o) : t.prototype && t.prototype.isPureReactComponent ? !Kr(n, r) || !Kr(i, a) : !0;
	}
	function Cc(e, t, n, r) {
		e = t.state, typeof t.componentWillReceiveProps == "function" && t.componentWillReceiveProps(n, r), typeof t.UNSAFE_componentWillReceiveProps == "function" && t.UNSAFE_componentWillReceiveProps(n, r), t.state !== e && xc.enqueueReplaceState(t, t.state, null);
	}
	function wc(e, t) {
		var n = t;
		if ("ref" in t) for (var r in n = {}, t) r !== "ref" && (n[r] = t[r]);
		if (e = e.defaultProps) for (var i in n === t && (n = T({}, n)), e) n[i] === void 0 && (n[i] = e[i]);
		return n;
	}
	function Tc(e) {
		Ci(e);
	}
	function Ec(e) {
		console.error(e);
	}
	function Dc(e) {
		Ci(e);
	}
	function Oc(e, t) {
		try {
			var n = e.onUncaughtError;
			n(t.value, { componentStack: t.stack });
		} catch (e) {
			setTimeout(function() {
				throw e;
			});
		}
	}
	function kc(e, t, n) {
		try {
			var r = e.onCaughtError;
			r(n.value, {
				componentStack: n.stack,
				errorBoundary: t.tag === 1 ? t.stateNode : null
			});
		} catch (e) {
			setTimeout(function() {
				throw e;
			});
		}
	}
	function Ac(e, t, n) {
		return n = yo(n), n.tag = 3, n.payload = { element: null }, n.callback = function() {
			Oc(e, t);
		}, n;
	}
	function jc(e) {
		return e = yo(e), e.tag = 3, e;
	}
	function Mc(e, t, n, r) {
		var i = n.type.getDerivedStateFromError;
		if (typeof i == "function") {
			var a = r.value;
			e.payload = function() {
				return i(a);
			}, e.callback = function() {
				kc(t, n, r);
			};
		}
		var o = n.stateNode;
		o !== null && typeof o.componentDidCatch == "function" && (e.callback = function() {
			kc(t, n, r), typeof i != "function" && (vd === null ? vd = /* @__PURE__ */ new Set([this]) : vd.add(this));
			var e = r.stack;
			this.componentDidCatch(r.value, { componentStack: e === null ? "" : e });
		});
	}
	function Nc(e, t, n, r, i) {
		if (n.flags |= 32768, typeof r == "object" && r && typeof r.then == "function") {
			if (t = n.alternate, t !== null && wa(t, n, i, !0), n = No.current, n !== null) {
				switch (n.tag) {
					case 31:
					case 13:
					case 19: return Po === null ? Kd() : n.alternate === null && X === 0 && (X = 3), n.flags &= -257, n.flags |= 65536, n.lanes = i, r === eo ? n.flags |= 16384 : (t = n.updateQueue, t === null ? n.updateQueue = /* @__PURE__ */ new Set([r]) : t.add(r), mf(e, r, i)), !1;
					case 22: return n.flags |= 65536, r === eo ? n.flags |= 16384 : (t = n.updateQueue, t === null ? (t = {
						transitions: null,
						markerInstances: null,
						retryQueue: /* @__PURE__ */ new Set([r])
					}, n.updateQueue = t) : (n = t.retryQueue, n === null ? t.retryQueue = /* @__PURE__ */ new Set([r]) : n.add(r)), mf(e, r, i)), !1;
				}
				throw Error(s(435, n.tag));
			}
			return mf(e, r, i), Kd(), !1;
		}
		if (R) return t = No.current, t === null ? (r !== la && (t = Error(s(423), { cause: r }), ga(Wi(t, n))), e = e.current.alternate, e.flags |= 65536, i &= -i, e.lanes |= i, r = Wi(r, n), i = Ac(e.stateNode, r, i), So(e, i), X !== 4 && (X = 2)) : (!(t.flags & 65536) && (t.flags |= 256), t.flags |= 65536, t.lanes = i, r !== la && (e = Error(s(422), { cause: r }), ga(Wi(e, n)))), !1;
		var a = Error(s(520), { cause: r });
		if (a = Wi(a, n), dd === null ? dd = [a] : dd.push(a), X !== 4 && (X = 2), t === null) return !0;
		r = Wi(r, n), n = t;
		do {
			switch (n.tag) {
				case 3: return n.flags |= 65536, e = i & -i, n.lanes |= e, e = Ac(n.stateNode, r, e), So(n, e), !1;
				case 1:
					if (t = n.type, a = n.stateNode, !(n.flags & 128) && (typeof t.getDerivedStateFromError == "function" || a !== null && typeof a.componentDidCatch == "function" && (vd === null || !vd.has(a)))) return n.flags |= 65536, i &= -i, n.lanes |= i, i = jc(i), Mc(i, e, n, r), So(n, i), !1;
					break;
				case 22: if (n.memoizedState !== null) return n.flags |= 65536, !1;
			}
			n = n.return;
		} while (n !== null);
		return !1;
	}
	var Pc = Error(s(461)), Fc = !1;
	function Ic(e, t, n, r) {
		t.child = e === null ? ho(t, null, n, r) : mo(t, e.child, n, r);
	}
	function Lc(e, t, n, r, i) {
		n = n.render;
		var a = t.ref;
		if ("ref" in r) {
			var o = {};
			for (var s in r) s !== "ref" && (o[s] = r[s]);
		} else o = r;
		return Ea(t), r = es(e, t, n, o, a, i), s = is(), e !== null && !Fc ? (as(e, t, i), dl(e, t, i)) : (R && s && na(t), t.flags |= 1, Ic(e, t, r, i), t.child);
	}
	function Rc(e, t, n, r, i) {
		if (e === null) {
			var a = n.type;
			return typeof a == "function" && !Ii(a) && a.defaultProps === void 0 && n.compare === null ? (t.tag = 15, t.type = a, zc(e, t, a, r, i)) : (e = L(n.type, null, r, t, t.mode, i), e.ref = t.ref, e.return = t, t.child = e);
		}
		if (a = e.child, !fl(e, i)) {
			var o = a.memoizedProps;
			if (n = n.compare, n = n === null ? Kr : n, n(o, r) && e.ref === t.ref) return dl(e, t, i);
		}
		return t.flags |= 1, e = Li(a, r), e.ref = t.ref, e.return = t, t.child = e;
	}
	function zc(e, t, n, r, i) {
		if (e !== null) {
			var a = e.memoizedProps;
			if (Kr(a, r) && e.ref === t.ref) {
				if (Fc = !1, t.pendingProps = r = a, fl(e, i)) e.flags & 131072 && (Fc = !0);
				else return t.lanes = e.lanes, dl(e, t, i);
			}
		}
		return qc(e, t, n, r, i);
	}
	function Bc(e, t, n, r) {
		var i = r.children, a = e === null ? null : e.memoizedState;
		if (e === null && t.stateNode === null && (t.stateNode = {
			_visibility: 1,
			_pendingMarkers: null,
			_retryCache: null,
			_transitions: null
		}), r.mode === "hidden") {
			if (t.flags & 128) {
				if (a = a === null ? n : a.baseLanes | n, e !== null) {
					for (r = t.child = e.child, i = 0; r !== null;) i = i | r.lanes | r.childLanes, r = r.sibling;
					r = i & ~a;
				} else r = 0, t.child = null;
				return Hc(e, t, a, n, r);
			}
			if (n & 536870912) t.memoizedState = {
				baseLanes: 0,
				cachePool: null
			}, e !== null && Ya(t, a === null ? null : a.cachePool), a === null ? jo() : Ao(t, a), Lo(t);
			else return r = t.lanes = 536870912, Hc(e, t, a === null ? n : a.baseLanes | n, n, r);
		} else a === null ? (e !== null && Ya(t, null), jo(), Ro()) : (Ya(t, a.cachePool), Ao(t, a), Ro(), t.memoizedState = null);
		return Ic(e, t, i, n), t.child;
	}
	function Vc(e, t) {
		return e !== null && e.tag === 22 || t.stateNode !== null || (t.stateNode = {
			_visibility: 1,
			_pendingMarkers: null,
			_retryCache: null,
			_transitions: null
		}), t.sibling;
	}
	function Hc(e, t, n, r, i) {
		var a = Ja();
		return a = a === null ? null : {
			parent: Na._currentValue,
			pool: a
		}, t.memoizedState = {
			baseLanes: n,
			cachePool: a
		}, e !== null && Ya(t, null), jo(), Lo(t), e !== null && wa(e, t, r, !0), t.childLanes = i, null;
	}
	function Uc(e, t) {
		return t = nl({
			mode: t.mode,
			children: t.children
		}, e.mode), t.ref = e.ref, e.child = t, t.return = e, t;
	}
	function Wc(e, t, n) {
		return mo(t, e.child, null, n), e = Uc(t, t.pendingProps), e.flags |= 2, zo(t), t.memoizedState = null, e;
	}
	function Gc(e, t, n) {
		var r = t.pendingProps, i = !!(t.flags & 128);
		if (t.flags &= -129, e === null) {
			if (R) {
				if (r.mode === "hidden") return e = Uc(t, r), t.lanes = 536870912, e.memoizedState = {
					baseLanes: 0,
					cachePool: null
				}, Vc(null, e);
				if (Io(t), (e = oa) ? (e = am(e, ca), e = e !== null && e.data === "&" ? e : null, e !== null && (t.memoizedState = {
					dehydrated: e,
					treeContext: Zi === null ? null : {
						id: Qi,
						overflow: $i
					},
					retryLane: 536870912,
					hydrationErrors: null
				}, n = Vi(e), n.return = t, t.child = n, aa = t, oa = null)) : e = null, e === null) throw ua(t);
				return t.lanes = 536870912, null;
			}
			return Uc(t, r);
		}
		var a = e.memoizedState;
		if (a !== null) {
			var o = a.dehydrated;
			if (Io(t), i) {
				if (t.flags & 256) t.flags &= -257, t = Wc(e, t, n);
				else if (t.memoizedState !== null) t.child = e.child, t.flags |= 128, t = null;
				else throw Error(s(558));
			} else if (Fc || wa(e, t, n, !1), i = (n & e.childLanes) !== 0, Fc || i) {
				if (Oo.current === null) {
					if (r = K, r !== null && (o = wt(r, n), o !== 0 && o !== a.retryLane)) throw a.retryLane = o, Ai(e, o), Pd(r, e, o), Pc;
					Kd();
				}
				t = Wc(e, t, n);
			} else e = a.treeContext, oa = lm(o.nextSibling), aa = t, R = !0, sa = null, ca = !1, e !== null && ia(t, e), t = Uc(t, r), t.flags |= 134221824;
			return t;
		}
		return e = Li(e.child, {
			mode: r.mode,
			children: r.children
		}), e.ref = t.ref, t.child = e, e.return = t, e;
	}
	function Kc(e, t) {
		var n = t.ref;
		if (n === null) e !== null && e.ref !== null && (t.flags |= 4194816);
		else {
			if (typeof n != "function" && typeof n != "object") throw Error(s(284));
			(e === null || e.ref !== n) && (t.flags |= 4194816);
		}
	}
	function qc(e, t, n, r, i) {
		return Ea(t), n = es(e, t, n, r, void 0, i), r = is(), e !== null && !Fc ? (as(e, t, i), dl(e, t, i)) : (R && r && na(t), t.flags |= 1, Ic(e, t, n, i), t.child);
	}
	function Jc(e, t, n, r, i, a) {
		return Ea(t), t.updateQueue = null, n = ns(t, r, n, i), ts(e), r = is(), e !== null && !Fc ? (as(e, t, a), dl(e, t, a)) : (R && r && na(t), t.flags |= 1, Ic(e, t, n, a), t.child);
	}
	function Yc(e, t, n, r, i) {
		if (Ea(t), t.stateNode === null) {
			var a = Ni, o = n.contextType;
			typeof o == "object" && o && (a = Da(o)), a = new n(r, a), t.memoizedState = a.state !== null && a.state !== void 0 ? a.state : null, a.updater = xc, t.stateNode = a, a._reactInternals = t, a = t.stateNode, a.props = r, a.state = t.memoizedState, a.refs = {}, _o(t), o = n.contextType, a.context = typeof o == "object" && o ? Da(o) : Ni, a.state = t.memoizedState, o = n.getDerivedStateFromProps, typeof o == "function" && (bc(t, n, o, r), a.state = t.memoizedState), typeof n.getDerivedStateFromProps == "function" || typeof a.getSnapshotBeforeUpdate == "function" || typeof a.UNSAFE_componentWillMount != "function" && typeof a.componentWillMount != "function" || (o = a.state, typeof a.componentWillMount == "function" && a.componentWillMount(), typeof a.UNSAFE_componentWillMount == "function" && a.UNSAFE_componentWillMount(), o !== a.state && xc.enqueueReplaceState(a, a.state, null), To(t, r, a, i), wo(), a.state = t.memoizedState), typeof a.componentDidMount == "function" && (t.flags |= 4194308), r = !0;
		} else if (e === null) {
			a = t.stateNode;
			var s = t.memoizedProps, c = wc(n, s);
			a.props = c;
			var l = a.context, u = n.contextType;
			o = Ni, typeof u == "object" && u && (o = Da(u));
			var d = n.getDerivedStateFromProps;
			u = typeof d == "function" || typeof a.getSnapshotBeforeUpdate == "function", s = t.pendingProps !== s, u || typeof a.UNSAFE_componentWillReceiveProps != "function" && typeof a.componentWillReceiveProps != "function" || (s || l !== o) && Cc(t, a, r, o), go = !1;
			var f = t.memoizedState;
			a.state = f, To(t, r, a, i), wo(), l = t.memoizedState, s || f !== l || go ? (typeof d == "function" && (bc(t, n, d, r), l = t.memoizedState), (c = go || Sc(t, n, c, r, f, l, o)) ? (u || typeof a.UNSAFE_componentWillMount != "function" && typeof a.componentWillMount != "function" || (typeof a.componentWillMount == "function" && a.componentWillMount(), typeof a.UNSAFE_componentWillMount == "function" && a.UNSAFE_componentWillMount()), typeof a.componentDidMount == "function" && (t.flags |= 4194308)) : (typeof a.componentDidMount == "function" && (t.flags |= 4194308), t.memoizedProps = r, t.memoizedState = l), a.props = r, a.state = l, a.context = o, r = c) : (typeof a.componentDidMount == "function" && (t.flags |= 4194308), r = !1);
		} else {
			a = t.stateNode, vo(e, t), o = t.memoizedProps, u = wc(n, o), a.props = u, d = t.pendingProps, f = a.context, l = n.contextType, c = Ni, typeof l == "object" && l && (c = Da(l)), s = n.getDerivedStateFromProps, (l = typeof s == "function" || typeof a.getSnapshotBeforeUpdate == "function") || typeof a.UNSAFE_componentWillReceiveProps != "function" && typeof a.componentWillReceiveProps != "function" || (o !== d || f !== c) && Cc(t, a, r, c), go = !1, f = t.memoizedState, a.state = f, To(t, r, a, i), wo();
			var p = t.memoizedState;
			o !== d || f !== p || go || e !== null && e.dependencies !== null && Ta(e.dependencies) ? (typeof s == "function" && (bc(t, n, s, r), p = t.memoizedState), (u = go || Sc(t, n, u, r, f, p, c) || e !== null && e.dependencies !== null && Ta(e.dependencies)) ? (l || typeof a.UNSAFE_componentWillUpdate != "function" && typeof a.componentWillUpdate != "function" || (typeof a.componentWillUpdate == "function" && a.componentWillUpdate(r, p, c), typeof a.UNSAFE_componentWillUpdate == "function" && a.UNSAFE_componentWillUpdate(r, p, c)), typeof a.componentDidUpdate == "function" && (t.flags |= 4), typeof a.getSnapshotBeforeUpdate == "function" && (t.flags |= 1024)) : (typeof a.componentDidUpdate != "function" || o === e.memoizedProps && f === e.memoizedState || (t.flags |= 4), typeof a.getSnapshotBeforeUpdate != "function" || o === e.memoizedProps && f === e.memoizedState || (t.flags |= 1024), t.memoizedProps = r, t.memoizedState = p), a.props = r, a.state = p, a.context = c, r = u) : (typeof a.componentDidUpdate != "function" || o === e.memoizedProps && f === e.memoizedState || (t.flags |= 4), typeof a.getSnapshotBeforeUpdate != "function" || o === e.memoizedProps && f === e.memoizedState || (t.flags |= 1024), r = !1);
		}
		return a = r, Kc(e, t), r = !!(t.flags & 128), a || r ? (a = t.stateNode, n = r && typeof n.getDerivedStateFromError != "function" ? null : a.render(), t.flags |= 1, e !== null && r ? (t.child = mo(t, e.child, null, i), t.child = mo(t, null, n, i)) : Ic(e, t, n, i), t.memoizedState = a.state, e = t.child) : e = dl(e, t, i), e;
	}
	function Xc(e, t, n, r) {
		return ma(), t.flags |= 256, Ic(e, t, n, r), t.child;
	}
	var Zc = {
		dehydrated: null,
		treeContext: null,
		retryLane: 0,
		hydrationErrors: null
	};
	function Qc(e) {
		return {
			baseLanes: e,
			cachePool: Xa()
		};
	}
	function $c(e, t, n) {
		return e = e === null ? 0 : e.childLanes & ~n, t && (e |= ld), e;
	}
	function el(e, t, n) {
		var r = t.pendingProps, i = !1, a = !!(t.flags & 128), o;
		if ((o = a) || (o = e !== null && e.memoizedState === null ? !1 : !!(Bo.current & 2)), o && (i = !0, t.flags &= -129), o = !!(t.flags & 32), t.flags &= -33, e === null) {
			if (R) {
				if (i ? Fo(t) : Ro(), (e = oa) ? (e = am(e, ca), e = e !== null && e.data !== "&" ? e : null, e !== null && (t.memoizedState = {
					dehydrated: e,
					treeContext: Zi === null ? null : {
						id: Qi,
						overflow: $i
					},
					retryLane: 536870912,
					hydrationErrors: null
				}, n = Vi(e), n.return = t, t.child = n, aa = t, oa = null)) : e = null, e === null) throw ua(t);
				return t.lanes = sm(e) ? 32 : 536870912, null;
			}
			return a = r.children, r = r.fallback, i ? (Ro(), i = t.mode, a = nl({
				mode: "hidden",
				children: a
			}, i), r = zi(r, i, n, null), a.return = t, r.return = t, a.sibling = r, t.child = a, r = t.child, r.memoizedState = Qc(n), r.childLanes = $c(e, o, n), t.memoizedState = Zc, Vc(null, r)) : (Fo(t), tl(t, a));
		}
		var s = e.memoizedState;
		if (s !== null) {
			var c = s.dehydrated;
			if (c !== null) return il(e, t, a, o, r, c, s, n);
		}
		return i ? (Ro(), i = r.fallback, a = t.mode, s = e.child, c = s.sibling, r = Li(s, {
			mode: "hidden",
			children: r.children
		}), r.subtreeFlags = s.subtreeFlags & 1206910976, c === null ? (i = zi(i, a, n, null), i.flags |= 2) : i = Li(c, i), i.return = t, r.return = t, r.sibling = i, t.child = r, Vc(null, r), r = t.child, i = e.child.memoizedState, i === null ? i = Qc(n) : (a = i.cachePool, a === null ? a = Xa() : (s = Na._currentValue, a = a.parent === s ? a : {
			parent: s,
			pool: s
		}), i = {
			baseLanes: i.baseLanes | n,
			cachePool: a
		}), r.memoizedState = i, r.childLanes = $c(e, o, n), t.memoizedState = Zc, Vc(e.child, r)) : (Fo(t), n = e.child, e = n.sibling, n = Li(n, {
			mode: "visible",
			children: r.children
		}), n.return = t, n.sibling = null, e !== null && (o = t.deletions, o === null ? (t.deletions = [e], t.flags |= 16) : o.push(e)), t.child = n, t.memoizedState = null, n);
	}
	function tl(e, t) {
		return t = nl({
			mode: "visible",
			children: t
		}, e.mode), t.return = e, e.child = t;
	}
	function nl(e, t) {
		return e = Fi(22, e, null, t), e.lanes = 0, e;
	}
	function rl(e, t, n) {
		return mo(t, e.child, null, n), e = tl(t, t.pendingProps.children), e.flags |= 2, t.memoizedState = null, e;
	}
	function il(e, t, n, r, i, a, o, c) {
		if (n) return t.flags & 256 ? (Fo(t), t.flags &= -257, rl(e, t, c)) : t.memoizedState === null ? (Ro(), a = i.fallback, o = t.mode, i = nl({
			mode: "visible",
			children: i.children
		}, o), a = zi(a, o, c, null), a.flags |= 2, i.return = t, a.return = t, i.sibling = a, t.child = i, mo(t, e.child, null, c), i = t.child, i.memoizedState = Qc(c), i.childLanes = $c(e, r, c), t.memoizedState = Zc, Vc(null, i)) : (Ro(), t.child = e.child, t.flags |= 128, null);
		if (Fo(t), sm(a)) {
			if (r = a.nextSibling && a.nextSibling.dataset, r) var l = r.dgst;
			return r = l, r !== "" && (i = Error(s(419)), i.stack = "", i.digest = r, ga({
				value: i,
				source: null,
				stack: null
			})), rl(e, t, c);
		}
		if (Fc || wa(e, t, c, !1), r = (c & e.childLanes) !== 0, Fc || r) {
			if (Oo.current !== null) return rl(e, t, c);
			if (r = K, r !== null && (i = wt(r, c), i !== 0 && i !== o.retryLane)) throw o.retryLane = i, Ai(e, i), Pd(r, e, i), Pc;
			return om(a) || Kd(), rl(e, t, c);
		}
		return om(a) ? (t.flags |= 192, t.child = e.child, null) : (e = o.treeContext, oa = lm(a.nextSibling), aa = t, R = !0, sa = null, ca = !1, e !== null && ia(t, e), t = tl(t, i.children), t.flags |= 134221824, t);
	}
	function al(e, t, n) {
		e.lanes |= t;
		var r = e.alternate;
		r !== null && (r.lanes |= t), Sa(e.return, t, n);
	}
	function ol(e) {
		for (var t = null; e !== null;) {
			var n = e.alternate;
			n !== null && Uo(n) === null && (t = e), e = e.sibling;
		}
		return t;
	}
	function sl(e, t, n, r, i, a) {
		var o = e.memoizedState;
		o === null ? e.memoizedState = {
			isBackwards: t,
			rendering: null,
			renderingStartTime: 0,
			last: r,
			tail: n,
			tailMode: i,
			treeForkCount: a
		} : (o.isBackwards = t, o.rendering = null, o.renderingStartTime = 0, o.last = r, o.tail = n, o.tailMode = i, o.treeForkCount = a);
	}
	function cl(e) {
		var t = e.child;
		for (e.child = null; t !== null;) {
			var n = t.sibling;
			t.sibling = e.child, e.child = t, t = n;
		}
	}
	function ll(e, t, n) {
		var r = t.pendingProps, i = r.revealOrder, a = r.tail;
		r = r.children;
		var o = Bo.current;
		if (t.flags & 128) return Vo(t, o), null;
		var s = !!(o & 2);
		if (s ? (o = o & 1 | 2, t.flags |= 128) : o &= 1, Vo(t, o), i === "backwards" && e !== null ? (cl(e), Ic(e, t, r, n), cl(e)) : Ic(e, t, r, n), r = R ? Ji : 0, !s && e !== null && e.flags & 128) a: for (e = t.child; e !== null;) {
			if (e.tag === 13) e.memoizedState !== null && al(e, n, t);
			else if (e.tag === 19) al(e, n, t);
			else if (e.child !== null) {
				e.child.return = e, e = e.child;
				continue;
			}
			if (e === t) break a;
			for (; e.sibling === null;) {
				if (e.return === null || e.return === t) break a;
				e = e.return;
			}
			e.sibling.return = e.return, e = e.sibling;
		}
		switch (i) {
			case "backwards":
				n = ol(t.child), n === null ? (i = t.child, t.child = null) : (i = n.sibling, n.sibling = null, cl(t)), sl(t, !0, i, null, a, r);
				break;
			case "unstable_legacy-backwards":
				for (n = null, i = t.child, t.child = null; i !== null;) {
					if (e = i.alternate, e !== null && Uo(e) === null) {
						t.child = i;
						break;
					}
					e = i.sibling, i.sibling = n, n = i, i = e;
				}
				sl(t, !0, n, null, a, r);
				break;
			case "together":
				sl(t, !1, null, null, void 0, r);
				break;
			case "independent":
				t.memoizedState = null;
				break;
			default: n = ol(t.child), n === null ? (i = t.child, t.child = null) : (i = n.sibling, n.sibling = null), sl(t, !1, i, n, a, r);
		}
		return t.child;
	}
	function ul(e, t, n) {
		var r = t.pendingProps;
		return ba(t, t.type, r.value), Ic(e, t, r.children, n), t.child;
	}
	function dl(e, t, n) {
		if (e !== null && (t.dependencies = e.dependencies), od |= t.lanes, (n & t.childLanes) === 0) {
			if (e !== null) {
				if (wa(e, t, n, !1), (n & t.childLanes) === 0) return null;
			} else return null;
		}
		if (e !== null && t.child !== e.child) throw Error(s(153));
		if (t.child !== null) {
			for (e = t.child, n = Li(e, e.pendingProps), t.child = n, n.return = t; e.sibling !== null;) e = e.sibling, n = n.sibling = Li(e, e.pendingProps), n.return = t;
			n.sibling = null;
		}
		return t.child;
	}
	function fl(e, t) {
		return (e.lanes & t) !== 0 || (e = e.dependencies, !!(e !== null && Ta(e)));
	}
	function pl(e, t, n) {
		switch (t.tag) {
			case 3:
				Me(t, t.stateNode.containerInfo), ba(t, Na, e.memoizedState.cache), ma();
				break;
			case 27:
			case 5:
				Pe(t);
				break;
			case 4:
				Me(t, t.stateNode.containerInfo);
				break;
			case 10:
				ba(t, t.type, t.memoizedProps.value);
				break;
			case 31:
				if (t.memoizedState !== null) return t.flags |= 128, Io(t), null;
				break;
			case 13:
				var r = t.memoizedState;
				if (r !== null) {
					if (r.dehydrated !== null) return Fo(t), t.flags |= 128, null;
					r = wa(e, t, n, !1);
					var i = t.child.childLanes;
					return r || (n & i) !== 0 ? el(e, t, n) : (Fo(t), e = dl(e, t, n), e === null ? null : e.sibling);
				}
				Fo(t);
				break;
			case 19:
				if (t.flags & 128) return ll(e, t, n);
				if (i = !!(e.flags & 128), r = (n & t.childLanes) !== 0, r ||= (wa(e, t, n, !1), (n & t.childLanes) !== 0), i) {
					if (r) return ll(e, t, n);
					t.flags |= 128;
				}
				if (i = t.memoizedState, i !== null && (i.rendering = null, i.tail = null, i.lastEffect = null), Vo(t, Bo.current), r) break;
				return null;
			case 22: return t.lanes = 0, Bc(e, t, n, t.pendingProps);
			case 24: ba(t, Na, e.memoizedState.cache);
		}
		return dl(e, t, n);
	}
	function ml(e, t, n) {
		if (e !== null) {
			if (e.memoizedProps !== t.pendingProps) Fc = !0;
			else {
				if (!fl(e, n) && !(t.flags & 128)) return Fc = !1, pl(e, t, n);
				Fc = !!(e.flags & 131072);
			}
		} else Fc = !1, R && t.flags & 1048576 && ta(t, Ji, t.index);
		switch (t.lanes = 0, t.tag) {
			case 16:
				a: {
					var r = t.pendingProps;
					if (e = ro(t.elementType), t.type = e, typeof e == "function") Ii(e) ? (r = wc(e, r), t.tag = 1, t = Yc(null, t, e, r, n)) : (t.tag = 0, t = qc(null, t, e, r, n));
					else {
						if (e != null) {
							var i = e.$$typeof;
							if (i === ce) {
								t.tag = 11, t = Lc(null, t, e, r, n);
								break a;
							}
							if (i === de) {
								t.tag = 14, t = Rc(null, t, e, r, n);
								break a;
							}
							if (i === se) {
								t.tag = 10, t.type = e, t = ul(null, t, n);
								break a;
							}
						}
						throw t = xe(e) || e, Error(s(306, t, ""));
					}
				}
				return t;
			case 0: return qc(e, t, t.type, t.pendingProps, n);
			case 1: return r = t.type, i = wc(r, t.pendingProps), Yc(e, t, r, i, n);
			case 3:
				a: {
					if (Me(t, t.stateNode.containerInfo), e === null) throw Error(s(387));
					r = t.pendingProps;
					var a = t.memoizedState;
					i = a.element, vo(e, t), To(t, r, null, n);
					var o = t.memoizedState;
					if (r = o.cache, ba(t, Na, r), r !== a.cache && Ca(t, [Na], n, !0), wo(), r = o.element, a.isDehydrated) {
						if (a = {
							element: r,
							isDehydrated: !1,
							cache: o.cache
						}, t.updateQueue.baseState = a, t.memoizedState = a, t.flags & 256) {
							t = Xc(e, t, r, n);
							break a;
						}
						if (r !== i) {
							i = Wi(Error(s(424)), t), ga(i), t = Xc(e, t, r, n);
							break a;
						}
						switch (e = t.stateNode.containerInfo, e.nodeType) {
							case 9:
								e = e.body;
								break;
							default: e = e.nodeName === "HTML" ? e.ownerDocument.body : e;
						}
						for (oa = lm(e.firstChild), aa = t, R = !0, sa = null, ca = !0, n = ho(t, null, r, n), t.child = n; n;) n.flags = n.flags & -3 | 134221824, n = n.sibling;
					} else {
						if (ma(), r === i) {
							t = dl(e, t, n);
							break a;
						}
						Ic(e, t, r, n);
					}
					t = t.child;
				}
				return t;
			case 26: return Kc(e, t), e === null ? (n = Nm(t.type, null, t.pendingProps, null)) ? t.memoizedState = n : R || (t.stateNode = fp(t.type, t.pendingProps, Ae.current, t)) : t.memoizedState = Nm(t.type, e.memoizedProps, t.pendingProps, e.memoizedState), null;
			case 27: return Pe(t), e === null && R && (r = t.stateNode = hm(t.type, t.pendingProps, Ae.current), aa = t, ca = !0, i = oa, Sp(t.type) ? (um = i, oa = lm(r.firstChild)) : oa = i), Ic(e, t, t.pendingProps.children, n), Kc(e, t), e === null && (t.flags |= 4194304), t.child;
			case 5: return e === null && R && ((i = r = oa) && (r = rm(r, t.type, t.pendingProps, ca), r === null ? i = !1 : (t.stateNode = r, aa = t, oa = lm(r.firstChild), ca = !1, i = !0)), i || ua(t)), Pe(t), i = t.type, a = t.pendingProps, o = e === null ? null : e.memoizedProps, r = a.children, pp(i, a) ? r = null : o !== null && pp(i, o) && (t.flags |= 32), t.memoizedState !== null && (i = es(e, t, rs, null, null, n), sh._currentValue = i), Kc(e, t), Ic(e, t, r, n), t.child;
			case 6: return e === null && R && ((e = n = oa) && (n = im(n, t.pendingProps, ca), n === null ? e = !1 : (t.stateNode = n, aa = t, oa = null, e = !0)), e || ua(t)), null;
			case 13: return el(e, t, n);
			case 4: return Me(t, t.stateNode.containerInfo), r = t.pendingProps, e === null ? t.child = mo(t, null, r, n) : Ic(e, t, r, n), t.child;
			case 11: return Lc(e, t, t.type, t.pendingProps, n);
			case 7: return r = t.pendingProps, Kc(e, t), Ic(e, t, r, n), t.child;
			case 8: return Ic(e, t, t.pendingProps.children, n), t.child;
			case 12: return Ic(e, t, t.pendingProps.children, n), t.child;
			case 10: return ul(e, t, n);
			case 9: return i = t.type._context, r = t.pendingProps.children, Ea(t), i = Da(i), r = r(i), t.flags |= 1, Ic(e, t, r, n), t.child;
			case 14: return Rc(e, t, t.type, t.pendingProps, n);
			case 15: return zc(e, t, t.type, t.pendingProps, n);
			case 19: return ll(e, t, n);
			case 31: return Gc(e, t, n);
			case 22: return Bc(e, t, n, t.pendingProps);
			case 24: return Ea(t), r = Da(Na), e === null ? (i = Ja(), i === null && (i = K, a = Pa(), i.pooledCache = a, a.refCount++, a !== null && (i.pooledCacheLanes |= n), i = a), t.memoizedState = {
				parent: r,
				cache: i
			}, _o(t), ba(t, Na, i)) : ((e.lanes & n) !== 0 && (vo(e, t), To(t, null, null, n), wo()), i = e.memoizedState, a = t.memoizedState, i.parent === r ? (r = a.cache, ba(t, Na, r), r !== i.cache && Ca(t, [Na], n, !0)) : (i = {
				parent: r,
				cache: r
			}, t.memoizedState = i, t.lanes === 0 && (t.memoizedState = t.updateQueue.baseState = i), ba(t, Na, r))), Ic(e, t, t.pendingProps.children, n), t.child;
			case 30: return t.stateNode === null && (t.stateNode = {
				autoName: null,
				paired: null,
				clones: null,
				ref: null
			}), r = t.pendingProps, r.name != null && r.name !== "auto" ? t.flags |= e === null ? 18882560 : 18874368 : R && na(t), e !== null && e.memoizedProps.name !== r.name ? t.flags |= 4194816 : Kc(e, t), Ic(e, t, r.children, n), t.child;
			case 29: throw t.pendingProps;
		}
		throw Error(s(156, t.tag));
	}
	function hl(e) {
		e.flags |= 4;
	}
	function gl(e, t, n, r, i) {
		var a;
		if ((a = !!(e.mode & 32)) && (a = n === null ? Jm(t, r) : Jm(t, r) && (r.src !== n.src || r.srcSet !== n.srcSet)), a) {
			if (e.flags |= 16777216, (i & 335544128) === i) {
				if (e.stateNode.complete) e.flags |= 8192;
				else if (Ud()) e.flags |= 8192;
				else throw io = eo, Qa;
			}
		} else e.flags &= -16777217;
	}
	function _l(e, t) {
		if (t.type !== "stylesheet" || t.state.loading & 4) e.flags &= -16777217;
		else if (e.flags |= 16777216, !Ym(t)) {
			if (Ud()) e.flags |= 8192;
			else throw io = eo, Qa;
		}
	}
	function vl(e, t) {
		t !== null && (e.flags |= 4), e.flags & 16384 && (t = e.tag === 22 ? 536870912 : vt(), e.lanes |= t, ud |= t);
	}
	function yl(e, t) {
		if (!R) switch (e.tailMode) {
			case "visible": break;
			case "collapsed":
				for (var n = e.tail, r = null; n !== null;) n.alternate !== null && (r = n), n = n.sibling;
				r === null ? t || e.tail === null ? e.tail = null : e.tail.sibling = null : r.sibling = null;
				break;
			default:
				for (t = e.tail, n = null; t !== null;) t.alternate !== null && (n = t), t = t.sibling;
				n === null ? e.tail = null : n.sibling = null;
		}
	}
	function H(e) {
		var t = e.alternate !== null && e.alternate.child === e.child, n = 0, r = 0;
		if (t) for (var i = e.child; i !== null;) n |= i.lanes | i.childLanes, r |= i.subtreeFlags & 1206910976, r |= i.flags & 1206910976, i.return = e, i = i.sibling;
		else for (i = e.child; i !== null;) n |= i.lanes | i.childLanes, r |= i.subtreeFlags, r |= i.flags, i.return = e, i = i.sibling;
		return e.subtreeFlags |= r, e.childLanes = n, t;
	}
	function bl(e, t, n) {
		var r = t.pendingProps;
		switch (ra(t), t.tag) {
			case 16:
			case 15:
			case 0:
			case 11:
			case 7:
			case 8:
			case 12:
			case 9:
			case 14: return H(t), null;
			case 1: return H(t), null;
			case 3: return n = t.stateNode, r = null, e !== null && (r = e.memoizedState.cache), t.memoizedState.cache !== r && (t.flags |= 2048), xa(Na), Ne(), n.pendingContext && (n.context = n.pendingContext, n.pendingContext = null), (e === null || e.child === null) && (pa(t) ? hl(t) : e === null || e.memoizedState.isDehydrated && !(t.flags & 256) || (t.flags |= 1024, ha())), H(t), null;
			case 26:
				var i = t.type, a = t.memoizedState;
				return e === null ? (hl(t), a === null ? (H(t), gl(t, i, null, r, n)) : (H(t), _l(t, a))) : a ? a === e.memoizedState ? (H(t), t.flags &= -16777217) : (hl(t), H(t), _l(t, a)) : (e = e.memoizedProps, e !== r && hl(t), H(t), gl(t, i, e, r, n)), null;
			case 27:
				if (Fe(t), n = Ae.current, i = t.type, e !== null && t.stateNode != null) e.memoizedProps !== r && hl(t);
				else {
					if (!r) {
						if (t.stateNode === null) throw Error(s(166));
						return H(t), t.subtreeFlags &= -33554433, null;
					}
					e = Oe.current, pa(t) ? da(t, e) : (e = hm(i, r, n), t.stateNode = e, hl(t));
				}
				return H(t), t.subtreeFlags &= -33554433, null;
			case 5:
				if (Fe(t), i = t.type, e !== null && t.stateNode != null) e.memoizedProps !== r && hl(t);
				else {
					if (!r) {
						if (t.stateNode === null) throw Error(s(166));
						return H(t), t.subtreeFlags &= -33554433, null;
					}
					if (a = Oe.current, pa(t)) da(t, a);
					else {
						var o = lp(Ae.current);
						switch (a) {
							case 1:
								a = o.createElementNS("http://www.w3.org/2000/svg", i);
								break;
							case 2:
								a = o.createElementNS("http://www.w3.org/1998/Math/MathML", i);
								break;
							default: switch (i) {
								case "svg":
									a = o.createElementNS("http://www.w3.org/2000/svg", i);
									break;
								case "math":
									a = o.createElementNS("http://www.w3.org/1998/Math/MathML", i);
									break;
								case "script":
									a = o.createElement("div"), a.innerHTML = "<script><\/script>", a = a.removeChild(a.firstChild);
									break;
								case "select":
									a = typeof r.is == "string" ? o.createElement("select", { is: r.is }) : o.createElement("select"), r.multiple ? a.multiple = !0 : r.size && (a.size = r.size);
									break;
								default: a = typeof r.is == "string" ? o.createElement(i, { is: r.is }) : o.createElement(i);
							}
						}
						a[At] = t, a[jt] = r;
						a: for (o = t.child; o !== null;) {
							if (o.tag === 5 || o.tag === 6) a.appendChild(o.stateNode);
							else if (o.tag !== 4 && o.tag !== 27 && o.child !== null) {
								o.child.return = o, o = o.child;
								continue;
							}
							if (o === t) break a;
							for (; o.sibling === null;) {
								if (o.return === null || o.return === t) break a;
								o = o.return;
							}
							o.sibling.return = o.return, o = o.sibling;
						}
						t.stateNode = a;
						a: switch (np(a, i, r), i) {
							case "button":
							case "input":
							case "select":
							case "textarea":
								r = !!r.autoFocus;
								break a;
							case "img":
								r = !0;
								break a;
							default: r = !1;
						}
						r && hl(t);
					}
				}
				return H(t), t.subtreeFlags &= -33554433, gl(t, t.type, e === null ? null : e.memoizedProps, t.pendingProps, n), null;
			case 6:
				if (e && t.stateNode != null) e.memoizedProps !== r && hl(t);
				else {
					if (typeof r != "string" && t.stateNode === null) throw Error(s(166));
					if (e = Ae.current, pa(t)) {
						if (e = t.stateNode, n = t.memoizedProps, r = null, i = aa, i !== null) switch (i.tag) {
							case 27:
							case 5: r = i.memoizedProps;
						}
						e[At] = t, e = !!(e.nodeValue === n || r !== null && !0 === r.suppressHydrationWarning || ep(e.nodeValue, n)), e || ua(t, !0);
					} else e = lp(e).createTextNode(r), e[At] = t, t.stateNode = e;
				}
				return H(t), null;
			case 31:
				if (n = t.memoizedState, e === null || e.memoizedState !== null) {
					if (r = pa(t), n !== null) {
						if (e === null) {
							if (!r) throw Error(s(318));
							if (e = t.memoizedState, e = e === null ? null : e.dehydrated, !e) throw Error(s(557));
							e[At] = t;
						} else ma(), !(t.flags & 128) && (t.memoizedState = null), t.flags |= 4;
						H(t), e = !1;
					} else n = ha(), e !== null && e.memoizedState !== null && (e.memoizedState.hydrationErrors = n), e = !0;
					if (!e) return t.flags & 256 ? (zo(t), t) : (zo(t), null);
					if (t.flags & 128) throw Error(s(558));
				}
				return H(t), null;
			case 13:
				if (r = t.memoizedState, e === null || e.memoizedState !== null && e.memoizedState.dehydrated !== null) {
					if (i = pa(t), r !== null && r.dehydrated !== null) {
						if (e === null) {
							if (!i) throw Error(s(318));
							if (i = t.memoizedState, i = i === null ? null : i.dehydrated, !i) throw Error(s(317));
							i[At] = t;
						} else ma(), !(t.flags & 128) && (t.memoizedState = null), t.flags |= 4;
						H(t), i = !1;
					} else i = ha(), e !== null && e.memoizedState !== null && (e.memoizedState.hydrationErrors = i), i = !0;
					if (!i) return t.flags & 256 ? (zo(t), t) : (zo(t), null);
				}
				return zo(t), t.flags & 128 ? (t.lanes = n, t) : (n = r !== null, e = e !== null && e.memoizedState !== null, n && (r = t.child, i = null, r.alternate !== null && r.alternate.memoizedState !== null && r.alternate.memoizedState.cachePool !== null && (i = r.alternate.memoizedState.cachePool.pool), a = null, r.memoizedState !== null && r.memoizedState.cachePool !== null && (a = r.memoizedState.cachePool.pool), a !== i && (r.flags |= 2048)), n !== e && n && (t.child.flags |= 8192), vl(t, t.updateQueue), H(t), null);
			case 4: return Ne(), e === null && Wf(t.stateNode.containerInfo), t.flags |= 67108864, H(t), null;
			case 10: return xa(t.type), H(t), null;
			case 19:
				if (Ho(t), r = t.memoizedState, r === null) return H(t), null;
				if (i = !!(t.flags & 128), a = r.rendering, a === null) {
					if (i) yl(r, !1);
					else {
						if (X !== 0 || e !== null && e.flags & 128) for (e = t.child; e !== null;) {
							if (a = Uo(e), a !== null) {
								for (t.flags |= 128, yl(r, !1), e = a.updateQueue, t.updateQueue = e, vl(t, e), t.subtreeFlags = 0, e = n, n = t.child; n !== null;) Ri(n, e), n = n.sibling;
								return Vo(t, Bo.current & 1 | 2), R && ea(t, r.treeForkCount), t.child;
							}
							e = e.sibling;
						}
						r.tail !== null && Je() > gd && (t.flags |= 128, i = !0, yl(r, !1), t.lanes = 4194304);
					}
				} else {
					if (!i) {
						if (e = Uo(a), e !== null) {
							if (t.flags |= 128, i = !0, e = e.updateQueue, t.updateQueue = e, vl(t, e), yl(r, !0), r.tail === null && r.tailMode !== "collapsed" && r.tailMode !== "visible" && !a.alternate && !R) return H(t), null;
						} else 2 * Je() - r.renderingStartTime > gd && n !== 536870912 && (t.flags |= 128, i = !0, yl(r, !1), t.lanes = 4194304);
					}
					r.isBackwards ? (a.sibling = t.child, t.child = a) : (e = r.last, e === null ? t.child = a : e.sibling = a, r.last = a);
				}
				if (r.tail !== null) {
					e = r.tail;
					a: {
						for (n = e; n !== null;) {
							if (n.alternate !== null) {
								n = !1;
								break a;
							}
							n = n.sibling;
						}
						n = !0;
					}
					return r.rendering = e, r.tail = e.sibling, r.renderingStartTime = Je(), e.sibling = null, a = Bo.current, a = i ? a & 1 | 2 : a & 1, r.tailMode === "visible" || r.tailMode === "collapsed" || !n || R ? Vo(t, a) : (n = a, A(No, t), A(Bo, n), Po === null && (Po = t)), R && ea(t, r.treeForkCount), e;
				}
				return H(t), null;
			case 22:
			case 23: return zo(t), Mo(), r = t.memoizedState !== null, e === null ? r && (t.flags |= 8192) : e.memoizedState !== null !== r && (t.flags |= 8192), r ? n & 536870912 && !(t.flags & 128) && (H(t), t.subtreeFlags & 6 && (t.flags |= 8192)) : H(t), n = t.updateQueue, n !== null && vl(t, n.retryQueue), n = null, e !== null && e.memoizedState !== null && e.memoizedState.cachePool !== null && (n = e.memoizedState.cachePool.pool), r = null, t.memoizedState !== null && t.memoizedState.cachePool !== null && (r = t.memoizedState.cachePool.pool), r !== n && (t.flags |= 2048), e !== null && De(qa), null;
			case 24: return n = null, e !== null && (n = e.memoizedState.cache), t.memoizedState.cache !== n && (t.flags |= 2048), xa(Na), H(t), null;
			case 25: return null;
			case 30: return t.flags |= 33554432, H(t), null;
		}
		throw Error(s(156, t.tag));
	}
	function xl(e, t) {
		switch (ra(t), t.tag) {
			case 1: return e = t.flags, e & 65536 ? (t.flags = e & -65537 | 128, t) : null;
			case 3: return xa(Na), Ne(), e = t.flags, e & 65536 && !(e & 128) ? (t.flags = e & -65537 | 128, t) : null;
			case 26:
			case 27:
			case 5: return Fe(t), null;
			case 31:
				if (t.memoizedState !== null) {
					if (zo(t), t.alternate === null) throw Error(s(340));
					ma();
				}
				return e = t.flags, e & 65536 ? (t.flags = e & -65537 | 128, t) : null;
			case 13:
				if (zo(t), e = t.memoizedState, e !== null && e.dehydrated !== null) {
					if (t.alternate === null) throw Error(s(340));
					ma();
				}
				return e = t.flags, e & 65536 ? (t.flags = e & -65537 | 128, t) : null;
			case 19: return Ho(t), e = t.flags, e & 65536 ? (t.flags = e & -65537 | 128, e = t.memoizedState, e !== null && (e.rendering = null, e.tail = null), t.flags |= 4, t) : null;
			case 4: return Ne(), null;
			case 10: return xa(t.type), null;
			case 22:
			case 23: return zo(t), Mo(), e !== null && De(qa), e = t.flags, e & 65536 ? (t.flags = e & -65537 | 128, t) : null;
			case 24: return xa(Na), null;
			case 25: return null;
			default: return null;
		}
	}
	function Sl(e, t) {
		switch (ra(t), t.tag) {
			case 3:
				xa(Na), Ne();
				break;
			case 26:
			case 27:
			case 5:
				Fe(t);
				break;
			case 4:
				Ne();
				break;
			case 31:
				t.memoizedState !== null && zo(t);
				break;
			case 13:
				zo(t);
				break;
			case 19:
				Ho(t);
				break;
			case 10:
				xa(t.type);
				break;
			case 22:
			case 23:
				zo(t), Mo(), e !== null && De(qa);
				break;
			case 24: xa(Na);
		}
	}
	function Cl(e, t) {
		try {
			var n = t.updateQueue, r = n === null ? null : n.lastEffect;
			if (r !== null) {
				var i = r.next;
				n = i;
				do {
					if ((n.tag & e) === e) {
						r = void 0;
						var a = n.create, o = n.inst;
						r = a(), o.destroy = r;
					}
					n = n.next;
				} while (n !== i);
			}
		} catch (e) {
			Z(t, t.return, e);
		}
	}
	function wl(e, t, n) {
		try {
			var r = t.updateQueue, i = r === null ? null : r.lastEffect;
			if (i !== null) {
				var a = i.next;
				r = a;
				do {
					if ((r.tag & e) === e) {
						var o = r.inst, s = o.destroy;
						if (s !== void 0) {
							o.destroy = void 0, i = t;
							var c = n, l = s;
							try {
								l();
							} catch (e) {
								Z(i, c, e);
							}
						}
					}
					r = r.next;
				} while (r !== a);
			}
		} catch (e) {
			Z(t, t.return, e);
		}
	}
	function Tl(e) {
		var t = e.updateQueue;
		if (t !== null) {
			var n = e.stateNode;
			try {
				Do(t, n);
			} catch (t) {
				Z(e, e.return, t);
			}
		}
	}
	function El(e, t, n) {
		n.props = wc(e.type, e.memoizedProps), n.state = e.memoizedState;
		try {
			n.componentWillUnmount();
		} catch (n) {
			Z(e, t, n);
		}
	}
	function Dl(e, t) {
		try {
			var n = e.ref;
			if (n !== null) {
				switch (e.tag) {
					case 26:
					case 27:
					case 5:
						var r = e.stateNode;
						break;
					case 30:
						var i = e.stateNode, a = bi(e.memoizedProps, i);
						(i.ref === null || i.ref.name !== a) && (i.ref = Pp(a)), r = i.ref;
						break;
					case 7:
						if (e.stateNode === null) {
							var o = new Fp(e);
							h(e.child, !1, Qp, o, void 0, void 0), e.stateNode = o;
						}
						r = e.stateNode;
						break;
					default: r = e.stateNode;
				}
				typeof n == "function" ? e.refCleanup = n(r) : n.current = r;
			}
		} catch (n) {
			Z(e, t, n);
		}
	}
	function U(e, t) {
		var n = e.ref, r = e.refCleanup;
		if (n !== null) {
			if (typeof r == "function") try {
				r();
			} catch (n) {
				Z(e, t, n);
			} finally {
				e.refCleanup = null, e = e.alternate, e != null && (e.refCleanup = null);
			}
			else if (typeof n == "function") try {
				n(null);
			} catch (n) {
				Z(e, t, n);
			}
			else n.current = null;
		}
	}
	function Ol(e, t) {
		if ((e.tag === 5 || e.tag === 27 || e.tag === 6) && e.alternate === null && t !== null) for (var n = 0; n < t.length; n++) em(e.stateNode, t[n]);
	}
	function kl(e) {
		for (var t = e.return; t !== null && (Ml(t) && em(e.stateNode, t.stateNode), !jl(t));) t = t.return;
	}
	function Al(e) {
		for (var t = e.return; t !== null && (Ml(t) && tm(e.stateNode, t.stateNode), !jl(t));) t = t.return;
	}
	function jl(e) {
		return e.tag === 5 || e.tag === 3 || e.tag === 27;
	}
	function Ml(e) {
		return e && e.tag === 7 && e.stateNode !== null;
	}
	function Nl(e) {
		var t = e.type, n = e.memoizedProps, r = e.stateNode;
		try {
			a: switch (t) {
				case "button":
				case "input":
				case "select":
				case "textarea":
					n.autoFocus && r.focus();
					break a;
				case "img": n.src ? r.src = n.src : n.srcSet && (r.srcset = n.srcSet);
			}
		} catch (t) {
			Z(e, e.return, t);
		}
	}
	function Pl(e, t, n) {
		try {
			var r = e.stateNode;
			ip(r, e.type, n, t), r[jt] = t;
		} catch (t) {
			Z(e, e.return, t);
		}
	}
	function Fl(e) {
		return e.tag === 5 || e.tag === 3 || e.tag === 26 || e.tag === 27 && Sp(e.type) || e.tag === 4;
	}
	function Il(e) {
		a: for (;;) {
			for (; e.sibling === null;) {
				if (e.return === null || Fl(e.return)) return null;
				e = e.return;
			}
			for (e.sibling.return = e.return, e = e.sibling; e.tag !== 5 && e.tag !== 6 && e.tag !== 18;) {
				if (e.tag === 27 && Sp(e.type) || e.flags & 2 || e.child === null || e.tag === 4) continue a;
				e.child.return = e, e = e.child;
			}
			if (!(e.flags & 2)) return e.stateNode;
		}
	}
	function Ll(e, t, n, r) {
		var i = e.tag;
		if (i === 5 || i === 6) i = e.stateNode, t ? (n.nodeType === 9 ? n.body : n.nodeName === "HTML" ? n.ownerDocument.body : n).insertBefore(i, t) : (t = n.nodeType === 9 ? n.body : n.nodeName === "HTML" ? n.ownerDocument.body : n, t.appendChild(i), n = n._reactRootContainer, n != null || t.onclick !== null || (t.onclick = Tn)), Ol(e, r), M = !0;
		else if (i !== 4 && (i === 27 && (Ol(e, r), r = null, Sp(e.type) && (n = e.stateNode, t = null)), e = e.child, e !== null)) for (Ll(e, t, n, r), e = e.sibling; e !== null;) Ll(e, t, n, r), e = e.sibling;
	}
	function Rl(e, t, n, r) {
		var i = e.tag;
		if (i === 5 || i === 6) i = e.stateNode, t ? n.insertBefore(i, t) : n.appendChild(i), Ol(e, r), M = !0;
		else if (i !== 4 && (i === 27 && (Ol(e, r), r = null, Sp(e.type) && (n = e.stateNode)), e = e.child, e !== null)) for (Rl(e, t, n, r), e = e.sibling; e !== null;) Rl(e, t, n, r), e = e.sibling;
	}
	function zl(e) {
		var t = e.stateNode, n = e.memoizedProps;
		try {
			for (var r = e.type, i = t.attributes; i.length;) t.removeAttributeNode(i[0]);
			np(t, r, n), t[At] = e, t[jt] = n;
		} catch (t) {
			Z(e, e.return, t);
		}
	}
	var Bl = !1, Vl = null;
	function Hl(e) {
		(e.tag === 30 || e.subtreeFlags & 33554432) && (Bl = !0);
	}
	var Ul = null;
	function Wl() {
		var e = Ul;
		return Ul = null, e;
	}
	var Gl = 0;
	function Kl(e, t, n, r, i) {
		return Gl = 0, ql(e.child, t, n, r, i);
	}
	function ql(e, t, n, r, i) {
		for (var a = !1; e !== null;) {
			if (e.tag === 5) {
				var o = e.stateNode;
				if (r !== null) {
					var s = Op(o);
					r.push(s), s.view && (a = !0);
				} else a || Op(o).view && (a = !0);
				Bl = !0, Tp(o, Gl === 0 ? t : t + "_" + Gl, n), Gl++;
			} else (e.tag !== 22 || e.memoizedState === null) && (e.tag === 30 && i || ql(e.child, t, n, r, i) && (a = !0));
			e = e.sibling;
		}
		return a;
	}
	function Jl(e, t) {
		for (; e !== null;) e.tag === 5 ? Ep(e.stateNode, e.memoizedProps) : (e.tag !== 22 || e.memoizedState === null) && (e.tag === 30 && t || Jl(e.child, t)), e = e.sibling;
	}
	function Yl(e) {
		if (e.subtreeFlags & 18874368) for (e = e.child; e !== null;) {
			if ((e.tag !== 22 || e.memoizedState === null) && (Yl(e), e.tag === 30 && e.flags & 18874368 && e.stateNode.paired)) {
				var t = e.memoizedProps;
				if (t.name == null || t.name === "auto") throw Error(s(544));
				var n = t.name;
				t = Si(t.default, t.share), t !== "none" && (Kl(e, n, t, null, !1) || Jl(e.child, !1));
			}
			e = e.sibling;
		}
	}
	function Xl(e, t) {
		if (e.tag === 30) {
			var n = e.stateNode, r = e.memoizedProps, i = bi(r, n), a = Si(r.default, n.paired ? r.share : r.enter);
			a === "none" ? Yl(e) : Kl(e, i, a, null, !1) ? (Yl(e), n.paired || t || Nd(e, r.onEnter)) : Jl(e.child, !1);
		} else if (e.subtreeFlags & 33554432) for (e = e.child; e !== null;) Xl(e, t), e = e.sibling;
		else Yl(e);
	}
	function Zl(e) {
		if (Vl !== null && Vl.size !== 0) {
			var t = Vl;
			if (e.subtreeFlags & 18874368) for (e = e.child; e !== null;) {
				if (e.tag !== 22 || e.memoizedState === null) {
					if (e.tag === 30 && e.flags & 18874368) {
						var n = e.memoizedProps, r = n.name;
						if (r != null && r !== "auto") {
							var i = t.get(r);
							if (i !== void 0) {
								var a = Si(n.default, n.share);
								if (a !== "none" && (Kl(e, r, a, null, !1) ? (a = e.stateNode, i.paired = a, a.paired = i, Nd(e, n.onShare)) : Jl(e.child, !1)), t.delete(r), t.size === 0) break;
							}
						}
					}
					Zl(e);
				}
				e = e.sibling;
			}
		}
	}
	function Ql(e) {
		if (e.tag === 30) {
			var t = e.memoizedProps, n = bi(t, e.stateNode), r = Vl === null ? void 0 : Vl.get(n), i = Si(t.default, r === void 0 ? t.exit : t.share);
			i !== "none" && (Kl(e, n, i, null, !1) ? r === void 0 ? Nd(e, t.onExit) : (i = e.stateNode, r.paired = i, i.paired = r, Vl.delete(n), Nd(e, t.onShare)) : Jl(e.child, !1)), Vl !== null && Zl(e);
		} else if (e.subtreeFlags & 33554432) for (e = e.child; e !== null;) Ql(e), e = e.sibling;
		else Vl !== null && Zl(e);
	}
	function $l(e) {
		for (e = e.child; e !== null;) {
			if (e.tag === 30) {
				var t = e.memoizedProps, n = bi(t, e.stateNode);
				t = Si(t.default, t.update), e.flags &= -5, t !== "none" && Kl(e, n, t, e.memoizedState = [], !1);
			} else e.subtreeFlags & 33554432 && $l(e);
			e = e.sibling;
		}
	}
	function eu(e) {
		if (e.subtreeFlags & 18874368) for (e = e.child; e !== null;) {
			if (e.tag !== 22 || e.memoizedState === null) {
				if (e.tag === 30 && e.flags & 18874368) {
					var t = e.stateNode;
					t.paired !== null && (t.paired = null, Jl(e.child, !1));
				}
				eu(e);
			}
			e = e.sibling;
		}
	}
	function tu(e) {
		if (e.tag === 30) e.stateNode.paired = null, Jl(e.child, !1), eu(e);
		else if (e.subtreeFlags & 33554432) for (e = e.child; e !== null;) tu(e), e = e.sibling;
		else eu(e);
	}
	function nu(e) {
		for (e = e.child; e !== null;) e.tag === 30 ? Jl(e.child, !1) : e.subtreeFlags & 33554432 && nu(e), e = e.sibling;
	}
	function ru(e, t, n, r, i, a, o) {
		for (var s = !1; t !== null;) {
			if (t.tag === 5) {
				var c = t.stateNode;
				if (a !== null && Gl < a.length) {
					var l = a[Gl], u = Op(c);
					(l.view || u.view) && (s = !0);
					var d;
					if (d = !(e.flags & 4)) {
						if (u.clip) d = !0;
						else {
							d = l.rect;
							var f = u.rect;
							d = d.y !== f.y || d.x !== f.x || d.height !== f.height || d.width !== f.width;
						}
					}
					d && (e.flags |= 4), u.abs ? u = !l.abs : (l = l.rect, u = u.rect, u = l.height !== u.height || l.width !== u.width), u && (e.flags |= 32);
				} else e.flags |= 32;
				e.flags & 4 && Tp(c, Gl === 0 ? n : n + "_" + Gl, i), s && e.flags & 4 || (Ul === null && (Ul = []), Ul.push(c, Gl === 0 ? r : r + "_" + Gl, t.memoizedProps)), Gl++;
			} else (t.tag !== 22 || t.memoizedState === null) && (t.tag === 30 && o ? e.flags |= t.flags & 32 : ru(e, t.child, n, r, i, a, o) && (s = !0));
			t = t.sibling;
		}
		return s;
	}
	function iu(e, t) {
		for (e = e.child; e !== null;) {
			if (e.tag === 30) {
				var n = e.memoizedProps, r = e.stateNode, i = bi(n, r), a = Si(n.default, n.update);
				if (t) {
					r = r.clones;
					var o = r === null ? null : r.map(kp);
				} else o = e.memoizedState, e.memoizedState = null;
				r = e;
				var s = e.child;
				Gl = 0, i = ru(r, s, i, i, a, o, !1), e.flags & 4 && i && (t || Nd(e, n.onUpdate));
			} else e.subtreeFlags & 33554432 && iu(e, t);
			e = e.sibling;
		}
	}
	var au = !1, W = !1, ou = !1, su = !1, cu = typeof WeakSet == "function" ? WeakSet : Set, lu = null, uu = !1, du = !1, fu = !1, pu = !1;
	function mu(e, t, n) {
		if (e = e.containerInfo, sp = gh, e = Zr(e), Qr(e)) {
			if ("selectionStart" in e) var r = {
				start: e.selectionStart,
				end: e.selectionEnd
			};
			else a: {
				r = (r = e.ownerDocument) && r.defaultView || window;
				var i = r.getSelection && r.getSelection();
				if (i && i.rangeCount !== 0) {
					r = i.anchorNode;
					var a = i.anchorOffset, o = i.focusNode;
					i = i.focusOffset;
					try {
						r.nodeType, o.nodeType;
					} catch {
						r = null;
						break a;
					}
					var s = 0, c = -1, l = -1, u = 0, d = 0, f = e, p = null;
					b: for (;;) {
						for (var m; f !== r || a !== 0 && f.nodeType !== 3 || (c = s + a), f !== o || i !== 0 && f.nodeType !== 3 || (l = s + i), f.nodeType === 3 && (s += f.nodeValue.length), (m = f.firstChild) !== null;) p = f, f = m;
						for (;;) {
							if (f === e) break b;
							if (p === r && ++u === a && (c = s), p === o && ++d === i && (l = s), (m = f.nextSibling) !== null) break;
							f = p, p = f.parentNode;
						}
						f = m;
					}
					r = c === -1 || l === -1 ? null : {
						start: c,
						end: l
					};
				} else r = null;
			}
			r ||= {
				start: 0,
				end: 0
			};
		} else r = null;
		for (cp = {
			focusedElem: e,
			selectionRange: r
		}, gh = !1, n = (n & 335544064) === n, lu = t, t = n ? 9270 : 1024; lu !== null;) {
			if (e = lu, n && (r = e.deletions, r !== null)) for (a = 0; a < r.length; a++) n && Ql(r[a]);
			if (e.alternate === null && e.flags & 2) n && Hl(e), hu(n);
			else {
				if (e.tag === 22) {
					if (r = e.alternate, e.memoizedState !== null) {
						r !== null && r.memoizedState === null && n && Ql(r), hu(n);
						continue;
					}
					if (r !== null && r.memoizedState !== null) {
						n && Hl(e), hu(n);
						continue;
					}
				}
				r = e.child, (e.subtreeFlags & t) !== 0 && r !== null ? (r.return = e, lu = r) : (n && $l(e), hu(n));
			}
		}
		Vl = null;
	}
	function hu(e) {
		for (; lu !== null;) {
			var t = lu, n = e, r = t.alternate, i = t.flags;
			switch (t.tag) {
				case 0:
				case 11:
				case 15: break;
				case 1:
					if (i & 1024 && r !== null) {
						n = void 0, i = r.memoizedProps, r = r.memoizedState;
						var a = t.stateNode;
						try {
							var o = wc(t.type, i);
							n = a.getSnapshotBeforeUpdate(o, r), a.__reactInternalSnapshotBeforeUpdate = n;
						} catch (e) {
							Z(t, t.return, e);
						}
					}
					break;
				case 3:
					if (i & 1024) {
						if (r = t.stateNode.containerInfo, n = r.nodeType, n === 9) nm(r);
						else if (n === 1) switch (r.nodeName) {
							case "HEAD":
							case "HTML":
							case "BODY":
								nm(r);
								break;
							default: r.textContent = "";
						}
					}
					break;
				case 5:
				case 26:
				case 27:
				case 6:
				case 4:
				case 17: break;
				case 30:
					n && r !== null && (n = bi(r.memoizedProps, r.stateNode), i = t.memoizedProps, i = Si(i.default, i.update), i !== "none" && Kl(r, n, i, r.memoizedState = [], !0));
					break;
				default: if (i & 1024) throw Error(s(163));
			}
			if (r = t.sibling, r !== null) {
				r.return = t.return, lu = r;
				break;
			}
			lu = t.return;
		}
	}
	function gu(e, t, n) {
		var r = n.flags;
		switch (n.tag) {
			case 0:
			case 11:
			case 15:
				Iu(e, n), r & 4 && Cl(5, n);
				break;
			case 1:
				if (Iu(e, n), r & 4) {
					if (e = n.stateNode, t === null) try {
						e.componentDidMount();
					} catch (e) {
						Z(n, n.return, e);
					}
					else {
						var i = wc(n.type, t.memoizedProps);
						t = t.memoizedState;
						try {
							e.componentDidUpdate(i, t, e.__reactInternalSnapshotBeforeUpdate);
						} catch (e) {
							Z(n, n.return, e);
						}
					}
				}
				r & 64 && Tl(n), r & 512 && Dl(n, n.return);
				break;
			case 3:
				if (Iu(e, n), r & 64 && (e = n.updateQueue, e !== null)) {
					if (t = null, n.child !== null) switch (n.child.tag) {
						case 27:
						case 5:
							t = n.child.stateNode;
							break;
						case 1: t = n.child.stateNode;
					}
					try {
						Do(e, t);
					} catch (e) {
						Z(n, n.return, e);
					}
				}
				break;
			case 27: t === null && r & 4 && zl(n);
			case 26:
			case 5:
				Iu(e, n), t === null && r & 4 && Nl(n), r & 512 && Dl(n, n.return);
				break;
			case 12:
				Iu(e, n);
				break;
			case 31:
				Iu(e, n), r & 4 && Tu(e, n);
				break;
			case 13:
				Iu(e, n), r & 4 && Eu(e, n), r & 64 && (e = n.memoizedState, e !== null && (e = e.dehydrated, e !== null && (n = _f.bind(null, n), cm(e, n))));
				break;
			case 22:
				if (r = n.memoizedState !== null || au, !r) {
					var a = t !== null && t.memoizedState !== null || W;
					t = au, i = W, au = r, (W = a) && !i ? (r = 2, n.subtreeFlags & 8772 && (r |= 1), Ru(e, n, r)) : Iu(e, n), au = t, W = i;
				}
				break;
			case 30:
				Iu(e, n), r & 512 && Dl(n, n.return);
				break;
			case 7: r & 512 && Dl(n, n.return);
			default: Iu(e, n);
		}
	}
	function _u(e, t) {
		for (e = e.child; e !== null;) vu(e, t), e = e.sibling;
	}
	function vu(e, t) {
		switch (e.tag) {
			case 5:
			case 26:
				try {
					var n = e.stateNode;
					if (t) {
						var r = n.style;
						typeof r.setProperty == "function" ? r.setProperty("display", "none", "important") : r.display = "none";
					} else {
						var i = e.stateNode, a = e.memoizedProps.style, o = a != null && a.hasOwnProperty("display") ? a.display : null;
						i.style.display = o == null || typeof o == "boolean" ? "" : ("" + o).trim();
					}
				} catch (t) {
					Z(e, e.return, t);
				}
				yu(e, t);
				break;
			case 6:
				try {
					e.stateNode.nodeValue = t ? "" : e.memoizedProps, M = !0;
				} catch (t) {
					Z(e, e.return, t);
				}
				break;
			case 18:
				try {
					var s = e.stateNode;
					t ? wp(s, !0) : wp(e.stateNode, !1);
				} catch (t) {
					Z(e, e.return, t);
				}
				break;
			case 22:
			case 23:
				e.memoizedState === null && _u(e, t);
				break;
			default: _u(e, t);
		}
	}
	function yu(e, t) {
		if (e.subtreeFlags & 67108864) for (e = e.child; e !== null;) {
			a: {
				var n = e, r = t;
				switch (n.tag) {
					case 4:
						vu(n, r);
						break a;
					case 22:
						n.memoizedState === null && yu(n, r);
						break a;
					default: yu(n, r);
				}
			}
			e = e.sibling;
		}
	}
	function bu(e) {
		var t = e.alternate;
		t !== null && (e.alternate = null, bu(t)), e.child = null, e.deletions = null, e.sibling = null, e.tag === 5 && (t = e.stateNode, t !== null && zt(t)), e.stateNode = null, e.return = null, e.dependencies = null, e.memoizedProps = null, e.memoizedState = null, e.pendingProps = null, e.stateNode = null, e.updateQueue = null;
	}
	var xu = null, Su = !1;
	function Cu(e, t, n) {
		for (n = n.child; n !== null;) wu(e, t, n), n = n.sibling;
	}
	function wu(e, t, n) {
		if (it && typeof it.onCommitFiberUnmount == "function") try {
			it.onCommitFiberUnmount(rt, n);
		} catch {}
		switch (n.tag) {
			case 26:
				W || U(n, t), Cu(e, t, n), n.memoizedState ? n.memoizedState.count-- : n.stateNode && !W && (n = n.stateNode, n.parentNode.removeChild(n));
				break;
			case 27:
				W || U(n, t), Al(n);
				var r = xu, i = Su;
				Sp(n.type) && (xu = n.stateNode, Su = !1), Cu(e, t, n), gm(n.stateNode, n.type, n.memoizedProps), xu = r, Su = i;
				break;
			case 5: W || U(n, t), Al(n);
			case 6:
				if (n.tag === 6 && Al(n), r = xu, i = Su, xu = null, Cu(e, t, n), xu = r, Su = i, xu !== null) {
					if (Su) try {
						(xu.nodeType === 9 ? xu.body : xu.nodeName === "HTML" ? xu.ownerDocument.body : xu).removeChild(n.stateNode), M = !0;
					} catch (e) {
						Z(n, t, e);
					}
					else try {
						xu.removeChild(n.stateNode), M = !0;
					} catch (e) {
						Z(n, t, e);
					}
				}
				break;
			case 18:
				xu !== null && (Su ? (e = xu, Cp(e.nodeType === 9 ? e.body : e.nodeName === "HTML" ? e.ownerDocument.body : e, n.stateNode), Hh(e)) : Cp(xu, n.stateNode));
				break;
			case 4:
				r = xu, i = Su, xu = n.stateNode.containerInfo, Su = !0, Cu(e, t, n), xu = r, Su = i;
				break;
			case 0:
			case 11:
			case 14:
			case 15:
				wl(2, n, t), W || wl(4, n, t), Cu(e, t, n);
				break;
			case 1:
				W || (U(n, t), r = n.stateNode, typeof r.componentWillUnmount == "function" && El(n, t, r)), Cu(e, t, n);
				break;
			case 21:
				Cu(e, t, n);
				break;
			case 22:
				W = (r = W) || n.memoizedState !== null, Cu(e, t, n), W = r;
				break;
			case 30:
				U(n, t), Cu(e, t, n);
				break;
			case 7:
				W || U(n, t), Cu(e, t, n);
				break;
			default: Cu(e, t, n);
		}
	}
	function Tu(e, t) {
		if (t.memoizedState === null && (e = t.alternate, e !== null && (e = e.memoizedState, e !== null))) {
			e = e.dehydrated;
			try {
				Hh(e);
			} catch (e) {
				Z(t, t.return, e);
			}
		}
	}
	function Eu(e, t) {
		if (t.memoizedState === null && (e = t.alternate, e !== null && (e = e.memoizedState, e !== null && (e = e.dehydrated, e !== null)))) try {
			Hh(e);
		} catch (e) {
			Z(t, t.return, e);
		}
	}
	function Du(e) {
		switch (e.tag) {
			case 31:
			case 13:
			case 19:
				var t = e.stateNode;
				return t === null && (t = e.stateNode = new cu()), t;
			case 22: return e = e.stateNode, t = e._retryCache, t === null && (t = e._retryCache = new cu()), t;
			default: throw Error(s(435, e.tag));
		}
	}
	function Ou(e, t) {
		var n = Du(e);
		t.forEach(function(t) {
			if (!n.has(t)) {
				n.add(t);
				var r = vf.bind(null, e, t);
				t.then(r, r);
			}
		});
	}
	function ku(e, t, n) {
		var r = t.deletions;
		if (r !== null) for (var i = 0; i < r.length; i++) {
			var a = r[i], o = e, c = t, l = c;
			a: for (; l !== null;) {
				switch (l.tag) {
					case 27:
						if (Sp(l.type)) {
							xu = l.stateNode, Su = !1;
							break a;
						}
						break;
					case 5:
						xu = l.stateNode, Su = !1;
						break a;
					case 3:
					case 4:
						xu = l.stateNode.containerInfo, Su = !0;
						break a;
				}
				l = l.return;
			}
			if (xu === null) throw Error(s(160));
			wu(o, c, a), xu = null, Su = !1, o = a.alternate, o !== null && (o.return = null), a.return = null;
		}
		if (t.subtreeFlags & 13886) for (t = t.child; t !== null;) ju(t, e, n), t = t.sibling;
	}
	var Au = null;
	function ju(e, t, n) {
		var r = e.alternate, i = e.flags;
		switch (e.tag) {
			case 0:
			case 11:
			case 14:
			case 15:
				if (i & 4 && (r = e.updateQueue, r = r === null ? null : r.events, r !== null)) for (var a = 0; a < r.length; a++) {
					var o = r[a];
					o.ref.impl = o.nextImpl;
				}
				ku(t, e, n), Mu(e), i & 4 && (wl(3, e, e.return), Cl(3, e), wl(5, e, e.return));
				break;
			case 1:
				ku(t, e, n), Mu(e), i & 512 && (W || r === null || U(r, r.return)), i & 64 && au && (e = e.updateQueue, e !== null && (t = e.callbacks, t !== null && (n = e.shared.hiddenCallbacks, e.shared.hiddenCallbacks = n === null ? t : n.concat(t))));
				break;
			case 26:
				if (a = Au, ku(t, e, n), Mu(e), i & 512 && (W || r === null || U(r, r.return)), i & 4) {
					if (i = r === null ? null : r.memoizedState, n = e.memoizedState, r === null) {
						if (n === null) {
							if (e.stateNode === null) {
								if (au) e.stateNode = fp(e.type, e.memoizedProps, t.containerInfo, e);
								else {
									a: {
										t = e.type, n = e.memoizedProps, i = a.ownerDocument || a;
										b: switch (t) {
											case "title":
												r = i.getElementsByTagName("title")[0], (!r || r[Lt] || r[At] || r.namespaceURI === "http://www.w3.org/2000/svg" || r.hasAttribute("itemprop")) && (r = i.createElement(t), i.head.insertBefore(r, i.querySelector("head > title"))), np(r, t, n), r[At] = e, Wt(r), t = r;
												break a;
											case "link":
												if (a = Gm("link", "href", i).get(t + (n.href || ""))) {
													for (o = 0; o < a.length; o++) if (r = a[o], r.getAttribute("href") === (n.href == null || n.href === "" ? null : n.href) && r.getAttribute("rel") === (n.rel == null ? null : n.rel) && r.getAttribute("title") === (n.title == null ? null : n.title) && r.getAttribute("crossorigin") === (n.crossOrigin == null ? null : n.crossOrigin)) {
														a.splice(o, 1);
														break b;
													}
												}
												r = i.createElement(t), np(r, t, n), i.head.appendChild(r);
												break;
											case "meta":
												if (a = Gm("meta", "content", i).get(t + (n.content || ""))) {
													for (o = 0; o < a.length; o++) if (r = a[o], r.getAttribute("content") === (n.content == null ? null : "" + n.content) && r.getAttribute("name") === (n.name == null ? null : n.name) && r.getAttribute("property") === (n.property == null ? null : n.property) && r.getAttribute("http-equiv") === (n.httpEquiv == null ? null : n.httpEquiv) && r.getAttribute("charset") === (n.charSet == null ? null : n.charSet)) {
														a.splice(o, 1);
														break b;
													}
												}
												r = i.createElement(t), np(r, t, n), i.head.appendChild(r);
												break;
											default: throw Error(s(468, t));
										}
										r[At] = e, Wt(r), t = r;
									}
									e.stateNode = t;
								}
							} else au || Km(a, e.type, e.stateNode);
						} else e.stateNode = Bm(a, n, e.memoizedProps);
					} else i === n ? n === null && e.stateNode !== null && Pl(e, e.memoizedProps, r.memoizedProps) : (i === null ? (t = r.stateNode, t === null || W || t.parentNode.removeChild(t)) : i.count--, n === null ? au || Km(a, e.type, e.stateNode) : Bm(a, n, e.memoizedProps));
				}
				break;
			case 27:
				ku(t, e, n), Mu(e), i & 512 && (W || r === null || U(r, r.return)), r !== null && i & 4 && Pl(e, e.memoizedProps, r.memoizedProps);
				break;
			case 5:
				if (a = ou, ou = !1, ku(t, e, n), ou = a, Mu(e), i & 512 && (W || r === null || U(r, r.return)), e.flags & 32) {
					t = e.stateNode;
					try {
						_n(t, ""), M = !0;
					} catch (t) {
						Z(e, e.return, t);
					}
				}
				i & 4 && e.stateNode != null && (t = e.memoizedProps, Pl(e, t, r === null ? t : r.memoizedProps)), i & 1024 && (su = !0);
				break;
			case 6:
				if (ku(t, e, n), Mu(e), i & 4) {
					if (e.stateNode === null) throw Error(s(162));
					t = e.memoizedProps, n = e.stateNode;
					try {
						n.nodeValue = t, M = !0;
					} catch (t) {
						Z(e, e.return, t);
					}
				}
				break;
			case 3:
				if (M = !1, Wm = null, a = Au, Au = bm(t.containerInfo), ku(t, e, n), Au = a, Mu(e), i & 4 && r !== null && r.memoizedState.isDehydrated) try {
					Hh(t.containerInfo);
				} catch (t) {
					Z(e, e.return, t);
				}
				su && (su = !1, Nu(e)), M = !1;
				break;
			case 4:
				i = ou, ou = au, r = $t(), a = Au, Au = bm(e.stateNode.containerInfo), ku(t, e, n), Mu(e), Au = a, M && du && (fu = !0), M = r, ou = i;
				break;
			case 12:
				ku(t, e, n), Mu(e);
				break;
			case 31:
				ku(t, e, n), Mu(e), i & 4 && (t = e.updateQueue, t !== null && (e.updateQueue = null, Ou(e, t)));
				break;
			case 13:
				ku(t, e, n), Mu(e), e.child.flags & 8192 && e.memoizedState !== null != (r !== null && r.memoizedState !== null) && (md = Je()), i & 4 && (t = e.updateQueue, t !== null && (e.updateQueue = null, Ou(e, t)));
				break;
			case 22:
				a = e.memoizedState !== null, o = r !== null && r.memoizedState !== null;
				var c = au, l = W, u = ou;
				au = c || a, ou = u || a, W = l || o, ku(t, e, n), W = l, ou = u, au = c, Mu(e), i & 8192 && (t = e.stateNode, t._visibility = a ? t._visibility & -2 : t._visibility | 1, !a || r === null || o || au || W || (t = o || W, n = au, r = W, au = a || au, W = t, Lu(e, 2), au = n, W = r), !a && ou || _u(e, a)), i & 4 && (t = e.updateQueue, t !== null && (n = t.retryQueue, n !== null && (t.retryQueue = null, Ou(e, n))));
				break;
			case 19:
				ku(t, e, n), Mu(e), i & 4 && (t = e.updateQueue, t !== null && (e.updateQueue = null, Ou(e, t)));
				break;
			case 30:
				i & 512 && (W || r === null || U(r, r.return)), i = $t(), a = du, o = (n & 335544064) === n, c = e.memoizedProps, du = o && Si(c.default, c.update) !== "none", ku(t, e, n), Mu(e), o && r !== null && M && (e.flags |= 4), du = a, M = i;
				break;
			case 21: break;
			case 7: i & 512 && (W || r === null || U(r, r.return)), r && r.stateNode !== null && (r.stateNode._fragmentFiber = e);
			default: ku(t, e, n), Mu(e);
		}
	}
	function Mu(e) {
		var t = e.flags;
		if (t & 2) {
			try {
				for (var n, r = e.return; r !== null;) {
					if (Fl(r)) {
						n = r;
						break;
					}
					r = r.return;
				}
				r = null;
				for (var i = e.return; i !== null;) {
					if (Ml(i)) {
						var a = i.stateNode;
						r === null ? r = [a] : r.push(a);
					}
					if (jl(i)) break;
					i = i.return;
				}
				var o = r;
				if (n == null) throw Error(s(160));
				switch (n.tag) {
					case 27:
						var c = n.stateNode;
						Rl(e, Il(e), c, o);
						break;
					case 5:
						var l = n.stateNode;
						n.flags & 32 && (_n(l, ""), n.flags &= -33), Rl(e, Il(e), l, o);
						break;
					case 3:
					case 4:
						var u = n.stateNode.containerInfo;
						Ll(e, Il(e), u, o);
						break;
					default: throw Error(s(161));
				}
			} catch (t) {
				Z(e, e.return, t);
			}
			e.flags &= -3;
		}
		t & 4096 && (e.flags &= -4097);
	}
	function Nu(e) {
		if (e.subtreeFlags & 1024) for (e = e.child; e !== null;) {
			var t = e;
			Nu(t), t.tag === 5 && t.flags & 1024 && (t = t.stateNode, gh = !0, t.reset(), gh = !1), e = e.sibling;
		}
	}
	function Pu(e, t) {
		if (t.subtreeFlags & 9270) for (t = t.child; t !== null;) Fu(t, e), t = t.sibling;
		else iu(t, !1);
	}
	function Fu(e, t) {
		var n = e.alternate;
		if (n === null) Xl(e, !1);
		else switch (e.tag) {
			case 3:
				if (pu = uu = !1, Wl(), Pu(t, e), !uu && !fu) {
					if (e = Ul, e !== null) for (var r = 0; r < e.length; r += 3) {
						n = e[r];
						var i = e[r + 1];
						Ep(n, e[r + 2]), n = n.ownerDocument.documentElement, n !== null && n.animate({
							opacity: [0, 0],
							pointerEvents: ["none", "none"]
						}, {
							duration: 0,
							fill: "forwards",
							pseudoElement: "::view-transition-group(" + i + ")"
						});
					}
					e = t.containerInfo, e = e.nodeType === 9 ? e.documentElement : e.ownerDocument.documentElement, e !== null && e.style.viewTransitionName === "" && (e.style.viewTransitionName = "none", e.animate({
						opacity: [0, 0],
						pointerEvents: ["none", "none"]
					}, {
						duration: 0,
						fill: "forwards",
						pseudoElement: "::view-transition-group(root)"
					}), e.animate({
						width: [0, 0],
						height: [0, 0]
					}, {
						duration: 0,
						fill: "forwards",
						pseudoElement: "::view-transition"
					})), pu = !0;
				}
				Ul = null;
				break;
			case 5:
				Pu(t, e);
				break;
			case 4:
				r = uu, uu = !1, Pu(t, e), uu && (fu = !0), uu = r;
				break;
			case 22:
				e.memoizedState === null && (n.memoizedState === null ? Pu(t, e) : Xl(e, !1));
				break;
			case 30:
				r = uu, i = Wl(), uu = !1, Pu(t, e), uu && (e.flags |= 4);
				var a = e.memoizedProps, o = e.stateNode;
				t = bi(a, o), o = bi(n.memoizedProps, o);
				var s = Si(a.default, a.update);
				s === "none" ? t = !1 : (a = n.memoizedState, n.memoizedState = null, n = e.child, Gl = 0, t = ru(e, n, t, o, s, a, !0), Gl !== (a === null ? 0 : a.length) && (e.flags |= 32)), e.flags & 4 && t ? (Nd(e, e.memoizedProps.onUpdate), Ul = i) : i !== null && (i.push.apply(i, Ul), Ul = i), uu = e.flags & 32 ? !0 : r;
				break;
			default: Pu(t, e);
		}
	}
	function Iu(e, t) {
		if (t.subtreeFlags & 8772) for (t = t.child; t !== null;) gu(e, t.alternate, t), t = t.sibling;
	}
	function Lu(e, t) {
		for (e = e.child; e !== null;) {
			var n = e, r = t;
			switch (n.tag) {
				case 0:
				case 11:
				case 14:
				case 15:
					wl(4, n, n.return), Lu(n, r);
					break;
				case 1:
					U(n, n.return);
					var i = n.stateNode;
					typeof i.componentWillUnmount == "function" && El(n, n.return, i), Lu(n, r);
					break;
				case 27: r & 2 && gm(n.stateNode, n.type, n.memoizedProps);
				case 5:
					U(n, n.return), n.tag !== 5 && n.tag !== 27 || Al(n), Lu(n, r);
					break;
				case 6:
					Al(n);
					break;
				case 26:
					U(n, n.return), i = n.stateNode, n.memoizedState !== null || i === null || W || i.parentNode.removeChild(i), Lu(n, r);
					break;
				case 22:
					n.memoizedState === null && Lu(n, r);
					break;
				case 30:
					U(n, n.return), Lu(n, r);
					break;
				case 7: U(n, n.return);
				default: Lu(n, r);
			}
			e = e.sibling;
		}
	}
	function Ru(e, t, n) {
		for (n = t.subtreeFlags & 8772 ? n : n & -2, t = t.child; t !== null;) {
			var r = t.alternate, i = e, a = t, o = a.flags, s = !!(n & 1);
			switch (a.tag) {
				case 0:
				case 11:
				case 15:
					Ru(i, a, n), Cl(4, a);
					break;
				case 1:
					if (Ru(i, a, n), r = a, i = r.stateNode, typeof i.componentDidMount == "function") try {
						i.componentDidMount();
					} catch (e) {
						Z(r, r.return, e);
					}
					if (r = a, i = r.updateQueue, i !== null) {
						var c = r.stateNode;
						try {
							var l = i.shared.hiddenCallbacks;
							if (l !== null) for (i.shared.hiddenCallbacks = null, i = 0; i < l.length; i++) Eo(l[i], c);
						} catch (e) {
							Z(r, r.return, e);
						}
					}
					s && o & 64 && Tl(a), Dl(a, a.return);
					break;
				case 27: n & 2 && zl(a);
				case 5:
					a.tag !== 5 && a.tag !== 27 || kl(a), Ru(i, a, n), s && r === null && o & 4 && Nl(a), Dl(a, a.return);
					break;
				case 6:
					kl(a);
					break;
				case 26:
					c = a.stateNode, a.memoizedState !== null || c === null || au || Km(bm(c.ownerDocument), a.type, c), Ru(i, a, n), s && r === null && o & 4 && Nl(a), Dl(a, a.return);
					break;
				case 12:
					Ru(i, a, n);
					break;
				case 31:
					Ru(i, a, n), s && o & 4 && Tu(i, a);
					break;
				case 13:
					Ru(i, a, n), s && o & 4 && Eu(i, a);
					break;
				case 22:
					a.memoizedState === null && Ru(i, a, n), Dl(a, a.return);
					break;
				case 30:
					Ru(i, a, n), Dl(a, a.return);
					break;
				case 7: Dl(a, a.return);
				default: Ru(i, a, n);
			}
			t = t.sibling;
		}
	}
	function zu(e, t) {
		var n = null;
		e !== null && e.memoizedState !== null && e.memoizedState.cachePool !== null && (n = e.memoizedState.cachePool.pool), e = null, t.memoizedState !== null && t.memoizedState.cachePool !== null && (e = t.memoizedState.cachePool.pool), e !== n && (e != null && e.refCount++, n != null && Fa(n));
	}
	function Bu(e, t) {
		e = null, t.alternate !== null && (e = t.alternate.memoizedState.cache), t = t.memoizedState.cache, t !== e && (t.refCount++, e != null && Fa(e));
	}
	function Vu(e, t, n, r) {
		var i = (n & 335544064) === n;
		if (t.subtreeFlags & (i ? 10262 : 10256)) for (t = t.child; t !== null;) Hu(e, t, n, r), t = t.sibling;
		else i && nu(t);
	}
	function Hu(e, t, n, r) {
		var i = (n & 335544064) === n;
		i && t.alternate === null && t.return !== null && t.return.alternate !== null && tu(t);
		var a = t.flags;
		switch (t.tag) {
			case 0:
			case 11:
			case 15:
				Vu(e, t, n, r), a & 2048 && Cl(9, t);
				break;
			case 1:
				Vu(e, t, n, r);
				break;
			case 3:
				Vu(e, t, n, r), i && pu && (e = e.containerInfo, e = e.nodeType === 9 ? e.body : e.nodeName === "HTML" ? e.ownerDocument.body : e, e.style.viewTransitionName === "root" && (e.style.viewTransitionName = ""), e = e.ownerDocument.documentElement, e !== null && e.style.viewTransitionName === "none" && (e.style.viewTransitionName = "")), a & 2048 && (a = null, t.alternate !== null && (a = t.alternate.memoizedState.cache), t = t.memoizedState.cache, t !== a && (t.refCount++, a != null && Fa(a)));
				break;
			case 12:
				if (a & 2048) {
					Vu(e, t, n, r), a = t.stateNode;
					try {
						var o = t.memoizedProps, s = o.id, c = o.onPostCommit;
						typeof c == "function" && c(s, t.alternate === null ? "mount" : "update", a.passiveEffectDuration, -0);
					} catch (e) {
						Z(t, t.return, e);
					}
				} else Vu(e, t, n, r);
				break;
			case 31:
				Vu(e, t, n, r);
				break;
			case 13:
				Vu(e, t, n, r);
				break;
			case 23: break;
			case 22:
				o = t.stateNode, s = t.alternate, t.memoizedState === null ? (i && s !== null && s.memoizedState !== null && tu(t), o._visibility & 2 ? Vu(e, t, n, r) : (o._visibility |= 2, Uu(e, t, n, r, !!(t.subtreeFlags & 10256) || !1))) : (i && s !== null && s.memoizedState === null && tu(s), o._visibility & 2 ? Vu(e, t, n, r) : Wu(e, t)), a & 2048 && zu(s, t);
				break;
			case 24:
				Vu(e, t, n, r), a & 2048 && Bu(t.alternate, t);
				break;
			case 30:
				i && (a = t.alternate, a !== null && (Jl(a.child, !0), Jl(t.child, !0))), Vu(e, t, n, r);
				break;
			default: Vu(e, t, n, r);
		}
	}
	function Uu(e, t, n, r, i) {
		for (i &&= !!(t.subtreeFlags & 10256) || !1, t = t.child; t !== null;) {
			var a = e, o = t, s = n, c = r, l = o.flags;
			switch (o.tag) {
				case 0:
				case 11:
				case 15:
					Uu(a, o, s, c, i), Cl(8, o);
					break;
				case 23: break;
				case 22:
					var u = o.stateNode;
					o.memoizedState === null ? (u._visibility |= 2, Uu(a, o, s, c, i)) : u._visibility & 2 ? Uu(a, o, s, c, i) : Wu(a, o), i && l & 2048 && zu(o.alternate, o);
					break;
				case 24:
					Uu(a, o, s, c, i), i && l & 2048 && Bu(o.alternate, o);
					break;
				default: Uu(a, o, s, c, i);
			}
			t = t.sibling;
		}
	}
	function Wu(e, t) {
		if (t.subtreeFlags & 10256) for (t = t.child; t !== null;) {
			var n = e, r = t, i = r.flags;
			switch (r.tag) {
				case 22:
					Wu(n, r), i & 2048 && zu(r.alternate, r);
					break;
				case 24:
					Wu(n, r), i & 2048 && Bu(r.alternate, r);
					break;
				default: Wu(n, r);
			}
			t = t.sibling;
		}
	}
	var Gu = 8192;
	function Ku(e, t, n) {
		if (e.subtreeFlags & Gu) for (e = e.child; e !== null;) qu(e, t, n), e = e.sibling;
	}
	function qu(e, t, n) {
		switch (e.tag) {
			case 26:
				Ku(e, t, n), e.flags & Gu && (e.memoizedState === null ? (e = e.stateNode, (t & 335544128) === t && Zm(n, e)) : Qm(n, Au, e.memoizedState, e.memoizedProps));
				break;
			case 5:
				Ku(e, t, n), e.flags & Gu && (e = e.stateNode, (t & 335544128) === t && Zm(n, e));
				break;
			case 3:
			case 4:
				var r = Au;
				Au = bm(e.stateNode.containerInfo), Ku(e, t, n), Au = r;
				break;
			case 22:
				e.memoizedState === null && (r = e.alternate, r !== null && r.memoizedState !== null ? (r = Gu, Gu = 16777216, Ku(e, t, n), Gu = r) : Ku(e, t, n));
				break;
			case 30:
				if ((e.flags & Gu) !== 0 && (r = e.memoizedProps.name, r != null && r !== "auto")) {
					var i = e.stateNode;
					i.paired = null, Vl === null && (Vl = /* @__PURE__ */ new Map()), Vl.set(r, i);
				}
				Ku(e, t, n);
				break;
			default: Ku(e, t, n);
		}
	}
	function Ju(e) {
		var t = e.alternate;
		if (t !== null && (e = t.child, e !== null)) {
			t.child = null;
			do
				t = e.sibling, e.sibling = null, e = t;
			while (e !== null);
		}
	}
	function Yu(e) {
		var t = e.deletions;
		if (e.flags & 16) {
			if (t !== null) for (var n = 0; n < t.length; n++) {
				var r = t[n];
				lu = r, Qu(r, e);
			}
			Ju(e);
		}
		if (e.subtreeFlags & 10256) for (e = e.child; e !== null;) Xu(e), e = e.sibling;
	}
	function Xu(e) {
		switch (e.tag) {
			case 0:
			case 11:
			case 15:
				Yu(e), e.flags & 2048 && wl(9, e, e.return);
				break;
			case 3:
				Yu(e);
				break;
			case 12:
				Yu(e);
				break;
			case 22:
				var t = e.stateNode;
				e.memoizedState !== null && t._visibility & 2 && (e.return === null || e.return.tag !== 13) ? (t._visibility &= -3, Zu(e)) : Yu(e);
				break;
			default: Yu(e);
		}
	}
	function Zu(e) {
		var t = e.deletions;
		if (e.flags & 16) {
			if (t !== null) for (var n = 0; n < t.length; n++) {
				var r = t[n];
				lu = r, Qu(r, e);
			}
			Ju(e);
		}
		for (e = e.child; e !== null;) {
			switch (t = e, t.tag) {
				case 0:
				case 11:
				case 15:
					wl(8, t, t.return), Zu(t);
					break;
				case 22:
					n = t.stateNode, n._visibility & 2 && (n._visibility &= -3, Zu(t));
					break;
				default: Zu(t);
			}
			e = e.sibling;
		}
	}
	function Qu(e, t) {
		for (; lu !== null;) {
			var n = lu;
			switch (n.tag) {
				case 0:
				case 11:
				case 15:
					wl(8, n, t);
					break;
				case 23:
				case 22:
					if (n.memoizedState !== null && n.memoizedState.cachePool !== null) {
						var r = n.memoizedState.cachePool.pool;
						r != null && r.refCount++;
					}
					break;
				case 24: Fa(n.memoizedState.cache);
			}
			if (r = n.child, r !== null) r.return = n, lu = r;
			else a: for (n = e; lu !== null;) {
				r = lu;
				var i = r.sibling, a = r.return;
				if (bu(r), r === n) {
					lu = null;
					break a;
				}
				if (i !== null) {
					i.return = a, lu = i;
					break a;
				}
				lu = a;
			}
		}
	}
	var $u = {
		getCacheForType: function(e) {
			var t = Da(Na), n = t.data.get(e);
			return n === void 0 && (n = e(), t.data.set(e, n)), n;
		},
		cacheSignal: function() {
			return Da(Na).controller.signal;
		}
	}, ed = typeof WeakMap == "function" ? WeakMap : Map, G = 0, K = null, q = null, J = 0, Y = 0, td = null, nd = !1, rd = !1, id = !1, ad = 0, X = 0, od = 0, sd = 0, cd = 0, ld = 0, ud = 0, dd = null, fd = null, pd = !1, md = 0, hd = 0, gd = Infinity, _d = null, vd = null, yd = 0, bd = null, xd = null, Sd = 0, Cd = 0, wd = null, Td = null, Ed = null, Dd = null, Od = null, kd = 0, Ad = null;
	function jd() {
		return G & 2 && J !== 0 ? J & -J : O.T === null ? Dt() : Pf();
	}
	function Md() {
		if (ld === 0) {
			if (!(J & 536870912) || R) {
				var e = dt;
				dt <<= 1, !(dt & 3932160) && (dt = 262144), ld = e;
			} else ld = 536870912;
		}
		return e = No.current, e !== null && (e.flags |= 32), ld;
	}
	function Nd(e, t) {
		if (t != null) {
			var n = e.stateNode, r = n.ref;
			r === null && (r = n.ref = Pp(bi(e.memoizedProps, n))), Dd === null && (Dd = []), Dd.push(t.bind(null, r));
		}
	}
	function Pd(e, t, n) {
		(e === K && (Y === 2 || Y === 9) || e.cancelPendingCommit !== null) && (Vd(e, 0), Rd(e, J, ld, !1)), bt(e, n), (!(G & 2) || e !== K) && (e === K && (!(G & 2) && (sd |= n), X === 4 && Rd(e, J, ld, !1)), Ef(e));
	}
	function Fd(e, t, n) {
		if (G & 6) throw Error(s(327));
		var r = !n && !(t & 127) && (t & e.expiredLanes) === 0 || ht(e, t), i = r ? Yd(e, t) : qd(e, t, !0), a = r;
		do {
			if (i === 0) {
				rd && !r && Rd(e, t, 0, !1);
				break;
			}
			if (n = e.current.alternate, a && !Ld(n)) {
				i = qd(e, t, !1), a = !1;
				continue;
			}
			if (i === 2) {
				if (a = t, e.errorRecoveryDisabledLanes & a) var o = 0;
				else o = e.pendingLanes & -536870913, o = o === 0 ? o & 536870912 ? 536870912 : 0 : o;
				if (o !== 0) {
					t = o;
					a: {
						var c = e;
						i = dd;
						var l = c.current.memoizedState.isDehydrated;
						if (l && (Vd(c, o).flags |= 256), o = qd(c, o, !1), o !== 2 && o !== 6) {
							if (id && !l) {
								c.errorRecoveryDisabledLanes |= a, sd |= a, i = 4;
								break a;
							}
							a = fd, fd = i, a !== null && (fd === null ? fd = a : fd.push.apply(fd, a));
						}
						i = o;
					}
					if (a = !1, i !== 2) continue;
				}
			}
			if (i === 1) {
				Vd(e, 0), Rd(e, t, 0, !0);
				break;
			}
			a: {
				switch (r = e, a = i, a) {
					case 0:
					case 1: throw Error(s(345));
					case 4: if ((t & 4194048) !== t && (t & 62914560) !== t) break;
					case 6:
						Rd(r, t, ld, !nd);
						break a;
					case 2:
						fd = null;
						break;
					case 3:
					case 5: break;
					default: throw Error(s(329));
				}
				if ((t & 62914560) === t && (i = md + 300 - Je(), 10 < i)) {
					if (Rd(r, t, ld, !nd), mt(r, 0, !0) !== 0) break a;
					Sd = t, r.timeoutHandle = gp(Id.bind(null, r, n, fd, _d, pd, t, ld, sd, ud, nd, a, "Throttled", -0, 0), i);
					break a;
				}
				Id(r, n, fd, _d, pd, t, ld, sd, ud, nd, a, null, -0, 0);
			}
			break;
		} while (1);
		Ef(e);
	}
	function Id(e, t, n, r, i, a, o, s, c, l, u, d, f, p) {
		e.timeoutHandle = -1;
		var m = t.subtreeFlags, h = (a & 335544064) === a;
		if (d = null, (h || m & 8192 || (m & 16785408) == 16785408) && (d = {
			stylesheets: null,
			count: 0,
			imgCount: 0,
			imgBytes: 0,
			suspenseyImages: [],
			waitingForImages: !0,
			waitingForViewTransition: !1,
			unsuspend: Tn
		}, Vl = null, qu(t, a, d), h && (m = d, h = e.containerInfo, h = (h.nodeType === 9 ? h : h.ownerDocument).__reactViewTransition, h != null && (m.count++, m.waitingForViewTransition = !0, m = nh.bind(m), h.finished.then(m, m))), m = (a & 62914560) === a ? md - Je() : (a & 4194048) === a ? hd - Je() : 0, m = eh(d, m), m !== null)) {
			Sd = a, e.cancelPendingCommit = m(nf.bind(null, e, t, a, n, r, i, o, s, c, l, u, d, null, f, p)), Rd(e, a, o, !l);
			return;
		}
		nf(e, t, a, n, r, i, o, s, c, l, u, d);
	}
	function Ld(e) {
		for (var t = e;;) {
			var n = t.tag;
			if ((n === 0 || n === 11 || n === 15) && t.flags & 16384 && (n = t.updateQueue, n !== null && (n = n.stores, n !== null))) for (var r = 0; r < n.length; r++) {
				var i = n[r], a = i.getSnapshot;
				i = i.value;
				try {
					if (!Gr(a(), i)) return !1;
				} catch {
					return !1;
				}
			}
			if (n = t.child, t.subtreeFlags & 16384 && n !== null) n.return = t, t = n;
			else {
				if (t === e) break;
				for (; t.sibling === null;) {
					if (t.return === null || t.return === e) return !0;
					t = t.return;
				}
				t.sibling.return = t.return, t = t.sibling;
			}
		}
		return !0;
	}
	function Rd(e, t, n, r) {
		t = gt(e, t), t &= ~cd, t &= ~sd, e.suspendedLanes |= t, e.pingedLanes &= ~t, r && (e.warmLanes |= t), r = e.expirationTimes;
		for (var i = t; 0 < i;) {
			var a = 31 - ot(i), o = 1 << a;
			r[a] = -1, i &= ~o;
		}
		n !== 0 && St(e, n, t);
	}
	function zd() {
		return G & 6 ? !0 : (Df(0, !1), !1);
	}
	function Bd() {
		if (q !== null) {
			if (Y === 0) var e = q.return;
			else e = q, ya = va = null, os(e), so = null, co = 0, e = q;
			for (; e !== null;) Sl(e.alternate, e), e = e.return;
			q = null;
		}
	}
	function Vd(e, t) {
		var n = e.timeoutHandle;
		return n !== -1 && (e.timeoutHandle = -1, _p(n)), n = e.cancelPendingCommit, n !== null && (e.cancelPendingCommit = null, n()), Sd = 0, Bd(), K = e, q = n = Li(e.current, null), J = t, Y = 0, td = null, nd = !1, rd = ht(e, t), id = !1, ud = ld = cd = sd = od = X = 0, fd = dd = null, pd = !1, ad = gt(e, t), Di(), n;
	}
	function Hd(e, t) {
		z = null, O.H = gc, t === Za || t === $a ? (t = ao(), Y = 3) : t === Qa ? (t = ao(), Y = 4) : Y = t === Pc ? 8 : typeof t == "object" && t && typeof t.then == "function" ? 6 : 1, td = t, q === null && (X = 1, Oc(e, Wi(t, e.current)));
	}
	function Ud() {
		var e = No.current;
		return e === null ? !0 : (J & 4194048) === J ? Po === null : (J & 62914560) === J || J & 536870912 ? e === Po : !1;
	}
	function Wd() {
		var e = O.H;
		return O.H = gc, e === null ? gc : e;
	}
	function Gd() {
		var e = O.A;
		return O.A = $u, e;
	}
	function Kd() {
		X = 4, nd || (J & 4194048) !== J && No.current !== null || (rd = !0), !(od & 134217727) && !(sd & 134217727) || K === null || Rd(K, J, ld, !1);
	}
	function qd(e, t, n) {
		var r = G;
		G |= 2;
		var i = Wd(), a = Gd();
		(K !== e || J !== t) && (_d = null, Vd(e, t)), t = !1;
		var o = X;
		a: do
			try {
				if (Y !== 0 && q !== null) {
					var s = q, c = td;
					switch (Y) {
						case 8:
							Bd(), o = 6;
							break a;
						case 3:
						case 2:
						case 9:
						case 6:
							No.current === null && (t = !0);
							var l = Y;
							if (Y = 0, td = null, $d(e, s, c, l), n && rd) {
								o = 0;
								break a;
							}
							break;
						default: l = Y, Y = 0, td = null, $d(e, s, c, l);
					}
				}
				Jd(), o = X;
				break;
			} catch (t) {
				Hd(e, t);
			}
		while (1);
		return t && e.shellSuspendCounter++, ya = va = null, G = r, O.H = i, O.A = a, q === null && (K = null, J = 0, Di()), o;
	}
	function Jd() {
		for (; q !== null;) Zd(q);
	}
	function Yd(e, t) {
		var n = G;
		G |= 2;
		var r = Wd(), i = Gd();
		K !== e || J !== t ? (_d = null, gd = Je() + 500, Vd(e, t)) : rd = ht(e, t);
		a: do
			try {
				if (Y !== 0 && q !== null) {
					t = q;
					var a = td;
					b: switch (Y) {
						case 1:
							Y = 0, td = null, $d(e, t, a, 1);
							break;
						case 2:
						case 9:
							if (to(a)) {
								Y = 0, td = null, Qd(t);
								break;
							}
							t = function() {
								Y !== 2 && Y !== 9 || K !== e || (Y = 7), Ef(e);
							}, a.then(t, t);
							break a;
						case 3:
							Y = 7;
							break a;
						case 4:
							Y = 5;
							break a;
						case 7:
							to(a) ? (Y = 0, td = null, Qd(t)) : (Y = 0, td = null, $d(e, t, a, 7));
							break;
						case 5:
							var o = null;
							switch (q.tag) {
								case 26: o = q.memoizedState;
								case 5:
								case 27:
									var c = q;
									if (o ? Ym(o) : c.stateNode.complete) {
										Y = 0, td = null;
										var l = c.sibling;
										if (l !== null) q = l;
										else {
											var u = c.return;
											u === null ? q = null : (q = u, ef(u));
										}
										break b;
									}
							}
							Y = 0, td = null, $d(e, t, a, 5);
							break;
						case 6:
							Y = 0, td = null, $d(e, t, a, 6);
							break;
						case 8:
							Bd(), X = 6;
							break a;
						default: throw Error(s(462));
					}
				}
				Xd();
				break;
			} catch (t) {
				Hd(e, t);
			}
		while (1);
		return ya = va = null, O.H = r, O.A = i, G = n, q === null ? (K = null, J = 0, Di(), X) : 0;
	}
	function Xd() {
		for (; q !== null && !Ke();) Zd(q);
	}
	function Zd(e) {
		var t = ml(e.alternate, e, ad);
		e.memoizedProps = e.pendingProps, t === null ? ef(e) : q = t;
	}
	function Qd(e) {
		var t = e, n = t.alternate;
		switch (t.tag) {
			case 15:
			case 0:
				t = Jc(n, t, t.pendingProps, t.type, void 0, J);
				break;
			case 11:
				t = Jc(n, t, t.pendingProps, t.type.render, t.ref, J);
				break;
			case 5:
				os(t);
				var r = t;
				r === aa && (R ? (fa(r), r.tag === 5 && r.stateNode != null && (oa = r.stateNode)) : (fa(r), R = !0));
			default: Sl(n, t), t = q = Ri(t, ad), t = ml(n, t, ad);
		}
		e.memoizedProps = e.pendingProps, t === null ? ef(e) : q = t;
	}
	function $d(e, t, n, r) {
		ya = va = null, os(t), so = null, co = 0;
		var i = t.return;
		try {
			if (Nc(e, i, t, n, J)) {
				X = 1, Oc(e, Wi(n, e.current)), q = null;
				return;
			}
		} catch (t) {
			if (i !== null) throw q = i, t;
			X = 1, Oc(e, Wi(n, e.current)), q = null;
			return;
		}
		t.flags & 32768 ? (R || r === 1 ? e = !0 : rd || J & 536870912 ? e = !1 : (nd = e = !0, (r === 2 || r === 9 || r === 3 || r === 6) && (r = No.current, r !== null && r.tag === 13 && (r.flags |= 16384))), tf(t, e)) : ef(t);
	}
	function ef(e) {
		var t = e;
		do {
			if (t.flags & 32768) {
				tf(t, nd);
				return;
			}
			e = t.return;
			var n = bl(t.alternate, t, ad);
			if (n !== null) {
				q = n;
				return;
			}
			if (t = t.sibling, t !== null) {
				q = t;
				return;
			}
			q = t = e;
		} while (t !== null);
		X === 0 && (X = 5);
	}
	function tf(e, t) {
		do {
			var n = xl(e.alternate, e);
			if (n !== null) {
				n.flags &= 32767, q = n;
				return;
			}
			if (n = e.return, n !== null && (n.flags |= 32768, n.subtreeFlags = 0, n.deletions = null), !t && (e = e.sibling, e !== null)) {
				q = e;
				return;
			}
			q = e = n;
		} while (e !== null);
		X = 6, q = null;
	}
	function nf(e, t, n, r, i, a, o, c, l, u, d, f) {
		e.cancelPendingCommit = null;
		do
			df();
		while (yd !== 0);
		if (G & 6) throw Error(s(327));
		if (t !== null) {
			if (t === e.current) throw Error(s(177));
			e === K && (q = K = null, J = 0), xd = t, bd = e, Sd = n, wd = i, Td = r, rf(e, t, n, o, c, l, f);
		}
	}
	function rf(e, t, n, r, i, a, o) {
		var s = t.lanes | t.childLanes;
		if (Cd = s, s |= Ei, xt(e, n, s, r, i, a), Dd = null, (n & 335544064) === n ? (Od = Ra(e), r = 10262) : (Od = null, r = 10256), (t.subtreeFlags & r) !== 0 || (t.flags & r) !== 0 ? (e.callbackNode = null, e.callbackPriority = 0, yf(Qe, function() {
			return ff(), null;
		})) : (e.callbackNode = null, e.callbackPriority = 0), Bl = !1, r = !!(t.flags & 13878), t.subtreeFlags & 13878 || r) {
			r = O.T, O.T = null, i = k.p, k.p = 2, a = G, G |= 4;
			try {
				mu(e, t, n);
			} finally {
				G = a, k.p = i, O.T = r;
			}
		}
		yd = 1, Bl ? Ed = Mp(o, e.containerInfo, Od, sf, cf, of, lf, ff, af, null, null) : (sf(), cf(), lf());
	}
	function af(e) {
		if (yd !== 0) {
			var t = bd.onRecoverableError;
			t(e, { componentStack: null });
		}
	}
	function of() {
		yd === 3 && (yd = 0, Fu(xd, bd), yd = 4);
	}
	function sf() {
		if (yd === 1) {
			yd = 0;
			var e = bd, t = xd, n = Sd, r = !!(t.flags & 13878);
			if (t.subtreeFlags & 13878 || r) {
				r = O.T, O.T = null;
				var i = k.p;
				k.p = 2;
				var a = G;
				G |= 4;
				try {
					du = fu = !1, ju(t, e, n), n = cp;
					var o = Zr(e.containerInfo), s = n.focusedElem, c = n.selectionRange;
					if (o !== s && s && s.ownerDocument && Xr(s.ownerDocument.documentElement, s)) {
						if (c !== null && Qr(s)) {
							var l = c.start, u = c.end;
							if (u === void 0 && (u = l), "selectionStart" in s) s.selectionStart = l, s.selectionEnd = Math.min(u, s.value.length);
							else {
								var d = s.ownerDocument || document, f = d && d.defaultView || window;
								if (f.getSelection) {
									var p = f.getSelection(), m = s.textContent.length, h = Math.min(c.start, m), g = c.end === void 0 ? h : Math.min(c.end, m);
									!p.extend && h > g && (o = g, g = h, h = o);
									var _ = Yr(s, h), v = Yr(s, g);
									if (_ && v && (p.rangeCount !== 1 || p.anchorNode !== _.node || p.anchorOffset !== _.offset || p.focusNode !== v.node || p.focusOffset !== v.offset)) {
										var y = d.createRange();
										y.setStart(_.node, _.offset), p.removeAllRanges(), h > g ? (p.addRange(y), p.extend(v.node, v.offset)) : (y.setEnd(v.node, v.offset), p.addRange(y));
									}
								}
							}
						}
						for (d = [], p = s; p = p.parentNode;) p.nodeType === 1 && d.push({
							element: p,
							left: p.scrollLeft,
							top: p.scrollTop
						});
						for (typeof s.focus == "function" && s.focus(), s = 0; s < d.length; s++) {
							var b = d[s];
							b.element.scrollLeft = b.left, b.element.scrollTop = b.top;
						}
					}
					gh = !!sp, cp = sp = null;
				} finally {
					G = a, k.p = i, O.T = r;
				}
			}
			e.current = t, yd = 2;
		}
	}
	function cf() {
		if (yd === 2) {
			yd = 0;
			var e = bd, t = xd, n = !!(t.flags & 8772);
			if (t.subtreeFlags & 8772 || n) {
				n = O.T, O.T = null;
				var r = k.p;
				k.p = 2;
				var i = G;
				G |= 4;
				try {
					gu(e, t.alternate, t);
				} finally {
					G = i, k.p = r, O.T = n;
				}
			}
			yd = 3;
		}
	}
	function lf() {
		if (yd === 4 || yd === 3) {
			yd = 0;
			var e = Ed;
			Ed = null, qe();
			var t = bd, n = xd, r = Sd, i = Td, a = (r & 335544064) === r ? 10262 : 10256;
			if ((n.subtreeFlags & a) !== 0 || (n.flags & a) !== 0 ? yd = 5 : (yd = 0, xd = bd = null, uf(t, t.pendingLanes)), a = t.pendingLanes, a === 0 && (vd = null), Et(r), n = n.stateNode, it && typeof it.onCommitFiberRoot == "function") try {
				it.onCommitFiberRoot(rt, n, void 0, (n.current.flags & 128) == 128);
			} catch {}
			if (i !== null) {
				n = O.T, a = k.p, k.p = 2, O.T = null;
				try {
					for (var o = t.onRecoverableError, s = 0; s < i.length; s++) {
						var c = i[s];
						o(c.value, { componentStack: c.stack });
					}
				} finally {
					O.T = n, k.p = a;
				}
			}
			if (i = Dd, o = Od, Od = null, i !== null && (Dd = null, o === null && (o = []), e !== null)) for (c = 0; c < i.length; c++) n = (0, i[c])(o), n !== void 0 && e.finished.finally(n);
			Sd & 3 && df(), Ef(t), a = t.pendingLanes, r & 261930 && a & 42 ? t === Ad ? kd++ : (kd = 0, Ad = t) : (kd = 0, Ad = null), Df(0, !1);
		}
	}
	function uf(e, t) {
		(e.pooledCacheLanes &= t) === 0 && (t = e.pooledCache, t != null && (e.pooledCache = null, Fa(t)));
	}
	function df() {
		return Ed !== null && (Ed.skipTransition(), Ed = null), sf(), cf(), lf(), ff();
	}
	function ff() {
		if (yd !== 5) return !1;
		var e = bd, t = Cd;
		Cd = 0;
		var n = Et(Sd), r = O.T, i = k.p;
		try {
			k.p = 32 > n ? 32 : n, O.T = null, n = wd, wd = null;
			var a = bd, o = Sd;
			if (yd = 0, xd = bd = null, Sd = 0, G & 6) throw Error(s(331));
			var c = G;
			if (G |= 4, Xu(a.current), Hu(a, a.current, o, n), G = c, Df(0, !1), it && typeof it.onPostCommitFiberRoot == "function") try {
				it.onPostCommitFiberRoot(rt, a);
			} catch {}
			return !0;
		} finally {
			k.p = i, O.T = r, uf(e, t);
		}
	}
	function pf(e, t, n) {
		t = Wi(n, t), t = Ac(e.stateNode, t, 2), e = bo(e, t, 2), e !== null && (bt(e, 2), Ef(e));
	}
	function Z(e, t, n) {
		if (e.tag === 3) pf(e, e, n);
		else for (; t !== null;) {
			if (t.tag === 3) {
				pf(t, e, n);
				break;
			}
			if (t.tag === 1) {
				var r = t.stateNode;
				if (typeof t.type.getDerivedStateFromError == "function" || typeof r.componentDidCatch == "function" && (vd === null || !vd.has(r))) {
					e = Wi(n, e), n = jc(2), r = bo(t, n, 2), r !== null && (Mc(n, r, t, e), bt(r, 2), Ef(r));
					break;
				}
			}
			t = t.return;
		}
	}
	function mf(e, t, n) {
		var r = e.pingCache;
		if (r === null) {
			r = e.pingCache = new ed();
			var i = /* @__PURE__ */ new Set();
			r.set(t, i);
		} else i = r.get(t), i === void 0 && (i = /* @__PURE__ */ new Set(), r.set(t, i));
		i.has(n) || (id = !0, i.add(n), e = hf.bind(null, e, t, n), t.then(e, e));
	}
	function hf(e, t, n) {
		var r = e.pingCache;
		r !== null && r.delete(t), e.pingedLanes |= e.suspendedLanes & n, e.warmLanes &= ~n, K === e && (J & n) === n && (X === 4 || X === 3 && (J & 62914560) === J && 300 > Je() - md ? G & 2 ? cd |= n : Vd(e, 0) : cd |= n, ud === J && (ud = 0)), Ef(e);
	}
	function gf(e, t) {
		t === 0 && (t = vt()), e = Ai(e, t), e !== null && (bt(e, t), Ef(e));
	}
	function _f(e) {
		var t = e.memoizedState, n = 0;
		t !== null && (n = t.retryLane), gf(e, n);
	}
	function vf(e, t) {
		var n = 0;
		switch (e.tag) {
			case 31:
			case 13:
				var r = e.stateNode, i = e.memoizedState;
				i !== null && (n = i.retryLane);
				break;
			case 19:
				r = e.stateNode;
				break;
			case 22:
				r = e.stateNode._retryCache;
				break;
			default: throw Error(s(314));
		}
		r !== null && r.delete(t), gf(e, n);
	}
	function yf(e, t) {
		return We(e, t);
	}
	var bf = null, xf = null, Sf = !1, Cf = !1, wf = !1, Tf = 0;
	function Ef(e) {
		e !== xf && e.next === null && (xf === null ? bf = xf = e : xf = xf.next = e), Cf = !0, Sf || (Sf = !0, Nf());
	}
	function Df(e, t) {
		if (!wf && Cf) {
			wf = !0;
			do
				for (var n = !1, r = bf; r !== null;) {
					if (!t) {
						if (e !== 0) {
							var i = r.pendingLanes;
							if (i === 0) var a = 0;
							else {
								var o = r.suspendedLanes, s = r.pingedLanes;
								a = (1 << 31 - ot(42 | e) + 1) - 1, a &= i & ~(o & ~s), a = a & 201326741 ? a & 201326741 | 1 : a ? a | 2 : 0;
							}
							a !== 0 && (n = !0, Mf(r, a));
						} else a = J, a = mt(r, r === K ? a : 0, r.cancelPendingCommit !== null || r.timeoutHandle !== -1), !(a & 3) || ht(r, a) || (n = !0, Mf(r, a));
					}
					r = r.next;
				}
			while (n);
			wf = !1;
		}
	}
	function Of() {
		kf();
	}
	function kf() {
		Cf = Sf = !1;
		var e = 0;
		Tf !== 0 && hp() && (e = Tf);
		for (var t = Je(), n = null, r = bf; r !== null;) {
			var i = r.next, a = Af(r, t);
			a === 0 ? (r.next = null, n === null ? bf = i : n.next = i, i === null && (xf = n)) : (n = r, (e !== 0 || a & 3) && (Cf = !0)), r = i;
		}
		yd !== 0 && yd !== 5 || Df(e, !1), Tf !== 0 && (Tf = 0);
	}
	function Af(e, t) {
		for (var n = e.suspendedLanes, r = e.pingedLanes, i = e.expirationTimes, a = e.pendingLanes & -62914561; 0 < a;) {
			var o = 31 - ot(a), s = 1 << o, c = i[o];
			c === -1 ? ((s & n) === 0 || (s & r) !== 0) && (i[o] = _t(s, t)) : c <= t && (e.expiredLanes |= s), a &= ~s;
		}
		if (t = K, n = J, n = mt(e, e === t ? n : 0, e.cancelPendingCommit !== null || e.timeoutHandle !== -1), r = e.callbackNode, n === 0 || e === t && (Y === 2 || Y === 9) || e.cancelPendingCommit !== null) return r !== null && r !== null && Ge(r), e.callbackNode = null, e.callbackPriority = 0;
		if (!(n & 3) || ht(e, n)) {
			if (t = n & -n, t === e.callbackPriority) return t;
			switch (r !== null && Ge(r), Et(n)) {
				case 2:
				case 8:
					n = Ze;
					break;
				case 32:
					n = Qe;
					break;
				case 268435456:
					n = et;
					break;
				default: n = Qe;
			}
			return r = jf.bind(null, e), n = We(n, r), e.callbackPriority = t, e.callbackNode = n, t;
		}
		return r !== null && r !== null && Ge(r), e.callbackPriority = 2, e.callbackNode = null, 2;
	}
	function jf(e, t) {
		if (yd !== 0 && yd !== 5) return e.callbackNode = null, e.callbackPriority = 0, null;
		var n = e.callbackNode;
		if (df() && e.callbackNode !== n) return null;
		var r = J;
		return r = mt(e, e === K ? r : 0, e.cancelPendingCommit !== null || e.timeoutHandle !== -1), r === 0 ? null : (Fd(e, r, t), Af(e, Je()), e.callbackNode != null && e.callbackNode === n ? jf.bind(null, e) : null);
	}
	function Mf(e, t) {
		if (df()) return null;
		Fd(e, t, !0);
	}
	function Nf() {
		bp(function() {
			G & 6 ? We(Xe, Of) : kf();
		});
	}
	function Pf() {
		if (Tf === 0) {
			var e = Va;
			e === 0 && (e = ut, ut <<= 1, !(ut & 261888) && (ut = 256)), Tf = e;
		}
		return Tf;
	}
	function Ff(e) {
		return e == null || typeof e == "symbol" || typeof e == "boolean" ? null : typeof e == "function" ? e : wn(e);
	}
	function If(e, t, n, r, i) {
		if (t === "submit" && n && n.stateNode === i) {
			var a = Ff((i[jt] || null).action), o = r.submitter;
			o && (t = (t = o[jt] || null) ? Ff(t.formAction) : o.getAttribute("formAction"), t !== null && (a = t, o = null));
			var s = new Un("action", "action", null, r, i);
			e.push({
				event: s,
				listeners: [{
					instance: null,
					listener: function() {
						if (r.defaultPrevented) {
							if (Tf !== 0) {
								var e = new FormData(i, o);
								nc(n, {
									pending: !0,
									data: e,
									method: i.method,
									action: a
								}, null, e);
							}
						} else typeof a == "function" && (s.preventDefault(), e = new FormData(i, o), nc(n, {
							pending: !0,
							data: e,
							method: i.method,
							action: a
						}, a, e));
					},
					currentTarget: i
				}]
			});
		}
	}
	for (var Lf = 0; Lf < _i.length; Lf++) {
		var Rf = _i[Lf];
		vi(Rf.toLowerCase(), "on" + (Rf[0].toUpperCase() + Rf.slice(1)));
	}
	vi(li, "onAnimationEnd"), vi(ui, "onAnimationIteration"), vi(di, "onAnimationStart"), vi("dblclick", "onDoubleClick"), vi("focusin", "onFocus"), vi("focusout", "onBlur"), vi(fi, "onTransitionRun"), vi(pi, "onTransitionStart"), vi(mi, "onTransitionCancel"), vi(hi, "onTransitionEnd"), j("onMouseEnter", ["mouseout", "mouseover"]), j("onMouseLeave", ["mouseout", "mouseover"]), j("onPointerEnter", ["pointerout", "pointerover"]), j("onPointerLeave", ["pointerout", "pointerover"]), Jt("onChange", "change click focusin focusout input keydown keyup selectionchange".split(" ")), Jt("onSelect", "focusout contextmenu dragend focusin keydown keyup mousedown mouseup selectionchange".split(" ")), Jt("onBeforeInput", [
		"compositionend",
		"keypress",
		"textInput",
		"paste"
	]), Jt("onCompositionEnd", "compositionend focusout keydown keypress keyup mousedown".split(" ")), Jt("onCompositionStart", "compositionstart focusout keydown keypress keyup mousedown".split(" ")), Jt("onCompositionUpdate", "compositionupdate focusout keydown keypress keyup mousedown".split(" "));
	var zf = "abort canplay canplaythrough durationchange emptied encrypted ended error loadeddata loadedmetadata loadstart pause play playing progress ratechange resize seeked seeking stalled suspend timeupdate volumechange waiting".split(" "), Bf = new Set("beforetoggle cancel close invalid load scroll scrollend toggle".split(" ").concat(zf));
	function Vf(e, t) {
		t = !!(t & 4);
		for (var n = 0; n < e.length; n++) {
			var r = e[n], i = r.event;
			r = r.listeners;
			a: {
				var a = void 0;
				if (t) for (var o = r.length - 1; 0 <= o; o--) {
					var s = r[o], c = s.instance, l = s.currentTarget;
					if (s = s.listener, c !== a && i.isPropagationStopped()) break a;
					a = s, i.currentTarget = l;
					try {
						a(i);
					} catch (e) {
						Ci(e);
					}
					i.currentTarget = null, a = c;
				}
				else for (o = 0; o < r.length; o++) {
					if (s = r[o], c = s.instance, l = s.currentTarget, s = s.listener, c !== a && i.isPropagationStopped()) break a;
					a = s, i.currentTarget = l;
					try {
						a(i);
					} catch (e) {
						Ci(e);
					}
					i.currentTarget = null, a = c;
				}
			}
		}
	}
	function Q(e, t) {
		var n = t[Nt];
		n === void 0 && (n = t[Nt] = /* @__PURE__ */ new Set());
		var r = e + "__bubble";
		n.has(r) || (Gf(t, e, 2, !1), n.add(r));
	}
	function Hf(e, t, n) {
		var r = 0;
		t && (r |= 4), Gf(n, e, r, t);
	}
	var Uf = "_reactListening" + Math.random().toString(36).slice(2);
	function Wf(e) {
		if (!e[Uf]) {
			e[Uf] = !0, Kt.forEach(function(t) {
				t !== "selectionchange" && (Bf.has(t) || Hf(t, !1, e), Hf(t, !0, e));
			});
			var t = e.nodeType === 9 ? e : e.ownerDocument;
			t === null || t[Uf] || (t[Uf] = !0, Hf("selectionchange", !1, t));
		}
	}
	function Gf(e, t, n, r) {
		switch (Ch(t)) {
			case 2:
				var i = _h;
				break;
			case 8:
				i = vh;
				break;
			default: i = yh;
		}
		n = i.bind(null, t, n, e), i = void 0, !Mn || t !== "touchstart" && t !== "touchmove" && t !== "wheel" || (i = !0), r ? i === void 0 ? e.addEventListener(t, n, !0) : e.addEventListener(t, n, {
			capture: !0,
			passive: i
		}) : i === void 0 ? e.addEventListener(t, n, !1) : e.addEventListener(t, n, { passive: i });
	}
	function Kf(e, t, n, r, i) {
		var a = r;
		if (!(t & 1) && !(t & 2) && r !== null) a: for (;;) {
			if (r === null) return;
			var o = r.tag;
			if (o === 3 || o === 4) {
				var s = r.stateNode.containerInfo;
				if (s === i) break;
				if (o === 4) for (o = r.return; o !== null;) {
					var c = o.tag;
					if ((c === 3 || c === 4) && o.stateNode.containerInfo === i) return;
					o = o.return;
				}
				for (; s !== null;) {
					if (o = Bt(s), o === null) return;
					if (c = o.tag, c === 5 || c === 6 || c === 26 || c === 27) {
						r = a = o;
						continue a;
					}
					s = s.parentNode;
				}
			}
			r = r.return;
		}
		An(function() {
			var r = a, i = Dn(n), o = [];
			a: {
				var s = gi.get(e);
				if (s !== void 0) {
					var c = Un, u = e;
					switch (e) {
						case "keypress": if (Rn(n) === 0) break a;
						case "keydown":
						case "keyup":
							c = sr;
							break;
						case "focusin":
							u = "focus", c = Qn;
							break;
						case "focusout":
							u = "blur", c = Qn;
							break;
						case "beforeblur":
						case "afterblur":
							c = Qn;
							break;
						case "click": if (n.button === 2) break a;
						case "auxclick":
						case "dblclick":
						case "mousedown":
						case "mousemove":
						case "mouseup":
						case "mouseout":
						case "mouseover":
						case "contextmenu":
							c = Xn;
							break;
						case "drag":
						case "dragend":
						case "dragenter":
						case "dragexit":
						case "dragleave":
						case "dragover":
						case "dragstart":
						case "drop":
							c = Zn;
							break;
						case "touchcancel":
						case "touchend":
						case "touchmove":
						case "touchstart":
							c = ur;
							break;
						case li:
						case ui:
						case di:
							c = $n;
							break;
						case hi:
							c = dr;
							break;
						case "scroll":
						case "scrollend":
							c = Gn;
							break;
						case "wheel":
							c = fr;
							break;
						case "copy":
						case "cut":
						case "paste":
							c = er;
							break;
						case "gotpointercapture":
						case "lostpointercapture":
						case "pointercancel":
						case "pointerdown":
						case "pointermove":
						case "pointerout":
						case "pointerover":
						case "pointerup":
							c = cr;
							break;
						case "submit":
							c = lr;
							break;
						case "toggle":
						case "beforetoggle": c = pr;
					}
					var d = !!(t & 4), f = !d && (e === "scroll" || e === "scrollend"), p = d ? s === null ? null : s + "Capture" : s;
					d = [];
					for (var m = r, h; m !== null;) {
						var g = m;
						if (h = g.stateNode, g = g.tag, g !== 5 && g !== 26 && g !== 27 || h === null || p === null || (g = F(m, p), g != null && d.push(qf(m, g, h))), f) break;
						m = m.return;
					}
					0 < d.length && (s = new c(s, u, null, n, i), o.push({
						event: s,
						listeners: d
					}));
				}
			}
			if (!(t & 7)) {
				a: {
					if (c = e === "mouseover" || e === "pointerover", s = e === "mouseout" || e === "pointerout", c && n !== En && (u = n.relatedTarget || n.fromElement) && (Bt(u) || u[Mt])) break a;
					(s || c) && (u = i.window === i ? i : (c = i.ownerDocument) ? c.defaultView || c.parentWindow : window, s ? (c = n.relatedTarget || n.toElement, s = r, c = c ? Bt(c) : null, c !== null && (f = l(c), d = c.tag, c !== f || d !== 5 && d !== 27 && d !== 6) && (c = null)) : (s = null, c = r), s !== c && (d = Xn, g = "onMouseLeave", p = "onMouseEnter", m = "mouse", (e === "pointerout" || e === "pointerover") && (d = cr, g = "onPointerLeave", p = "onPointerEnter", m = "pointer"), f = s == null ? u : Ht(s), h = c == null ? u : Ht(c), u = new d(g, m + "leave", s, n, i), u.target = f, u.relatedTarget = h, g = null, Bt(i) === r && (d = new d(p, m + "enter", c, n, i), d.target = h, d.relatedTarget = f, g = d), f = g, d = s && c ? te(s, c, Yf) : null, s !== null && Xf(o, u, s, d, !1), c !== null && f !== null && Xf(o, f, c, d, !0)));
				}
				a: {
					if (s = r ? Ht(r) : window, c = s.nodeName && s.nodeName.toLowerCase(), c === "select" || c === "input" && s.type === "file") var _ = Nr;
					else if (Dr(s)) {
						if (Pr) _ = Ur;
						else {
							_ = Vr;
							var v = Br;
						}
					} else c = s.nodeName, !c || c.toLowerCase() !== "input" || s.type !== "checkbox" && s.type !== "radio" ? r && xn(r.elementType) && (_ = Nr) : _ = Hr;
					if (_ &&= _(e, r)) {
						Or(o, _, n, i);
						break a;
					}
					v && v(e, s, r);
				}
				switch (v = r ? Ht(r) : window, e) {
					case "focusin":
						(Dr(v) || v.contentEditable === "true") && (ei = v, ti = r, ni = null);
						break;
					case "focusout":
						ni = ti = ei = null;
						break;
					case "mousedown":
						ri = !0;
						break;
					case "contextmenu":
					case "mouseup":
					case "dragend":
						ri = !1, ii(o, n, i);
						break;
					case "selectionchange": if ($r) break;
					case "keydown":
					case "keyup": ii(o, n, i);
				}
				var y;
				if (hr) b: {
					switch (e) {
						case "compositionstart":
							var b = "onCompositionStart";
							break b;
						case "compositionend":
							b = "onCompositionEnd";
							break b;
						case "compositionupdate":
							b = "onCompositionUpdate";
							break b;
					}
					b = void 0;
				}
				else Cr ? xr(e, n) && (b = "onCompositionEnd") : e === "keydown" && n.keyCode === 229 && (b = "onCompositionStart");
				b && (vr && n.locale !== "ko" && (Cr || b !== "onCompositionStart" ? b === "onCompositionEnd" && Cr && (y = Ln()) : (Pn = i, Fn = "value" in Pn ? Pn.value : Pn.textContent, Cr = !0)), v = Jf(r, b), 0 < v.length && (b = new tr(b, e, null, n, i), o.push({
					event: b,
					listeners: v
				}), y ? b.data = y : (y = Sr(n), y !== null && (b.data = y)))), (y = _r ? wr(e, n) : Tr(e, n)) && (b = Jf(r, "onBeforeInput"), 0 < b.length && (v = new tr("onBeforeInput", "beforeinput", null, n, i), o.push({
					event: v,
					listeners: b
				}), v.data = y)), If(o, e, r, n, i);
			}
			Vf(o, t);
		});
	}
	function qf(e, t, n) {
		return {
			instance: e,
			listener: t,
			currentTarget: n
		};
	}
	function Jf(e, t) {
		for (var n = t + "Capture", r = []; e !== null;) {
			var i = e, a = i.stateNode;
			if (i = i.tag, i !== 5 && i !== 26 && i !== 27 || a === null || (i = F(e, n), i != null && r.unshift(qf(e, i, a)), i = F(e, t), i != null && r.push(qf(e, i, a))), e.tag === 3) return r;
			e = e.return;
		}
		return [];
	}
	function Yf(e) {
		if (e === null) return null;
		do
			e = e.return;
		while (e && e.tag !== 5 && e.tag !== 27);
		return e || null;
	}
	function Xf(e, t, n, r, i) {
		for (var a = t._reactName, o = []; n !== null && n !== r;) {
			var s = n, c = s.alternate, l = s.stateNode;
			if (s = s.tag, c !== null && c === r) break;
			s !== 5 && s !== 26 && s !== 27 || l === null || (c = l, i ? (l = F(n, a), l != null && o.unshift(qf(n, l, c))) : i || (l = F(n, a), l != null && o.push(qf(n, l, c)))), n = n.return;
		}
		o.length !== 0 && e.push({
			event: t,
			listeners: o
		});
	}
	var Zf = /\r\n?/g, Qf = /\u0000|\uFFFD/g;
	function $f(e) {
		return (typeof e == "string" ? e : "" + e).replace(Zf, "\n").replace(Qf, "");
	}
	function ep(e, t) {
		return t = $f(t), $f(e) === t;
	}
	function $(e, t, n, r, i, a) {
		switch (n) {
			case "children":
				if (typeof r == "string") t === "body" || t === "textarea" && r === "" || _n(e, r);
				else if (typeof r == "number" || typeof r == "bigint") t !== "body" && _n(e, "" + r);
				else return;
				break;
			case "className":
				tn(e, "class", r);
				break;
			case "tabIndex":
				tn(e, "tabindex", r);
				break;
			case "dir":
			case "role":
			case "viewBox":
			case "width":
			case "height":
				tn(e, n, r);
				break;
			case "style":
				bn(e, r, a);
				return;
			case "data": if (t !== "object") {
				tn(e, "data", r);
				break;
			}
			case "src":
			case "href":
				if (r === "" && (t !== "a" || n !== "href")) {
					e.removeAttribute(n);
					break;
				}
				if (r == null || typeof r == "function" || typeof r == "symbol" || typeof r == "boolean") {
					e.removeAttribute(n);
					break;
				}
				r = wn(r), e.setAttribute(n, r);
				break;
			case "action":
			case "formAction":
				if (typeof r == "function") {
					e.setAttribute(n, "javascript:throw new Error('A React form was unexpectedly submitted. If you called form.submit() manually, consider using form.requestSubmit() instead. If you\\'re trying to use event.stopPropagation() in a submit event handler, consider also calling event.preventDefault().')");
					break;
				}
				if (typeof a == "function" && (n === "formAction" ? (t !== "input" && $(e, t, "name", i.name, i, null), $(e, t, "formEncType", i.formEncType, i, null), $(e, t, "formMethod", i.formMethod, i, null), $(e, t, "formTarget", i.formTarget, i, null)) : ($(e, t, "encType", i.encType, i, null), $(e, t, "method", i.method, i, null), $(e, t, "target", i.target, i, null))), r == null || typeof r == "symbol" || typeof r == "boolean") {
					e.removeAttribute(n);
					break;
				}
				r = wn(r), e.setAttribute(n, r);
				break;
			case "onClick":
				r != null && (e.onclick = Tn);
				return;
			case "onScroll":
				r != null && Q("scroll", e);
				return;
			case "onScrollEnd":
				r != null && Q("scrollend", e);
				return;
			case "dangerouslySetInnerHTML":
				if (r != null) {
					if (typeof r != "object" || !("__html" in r)) throw Error(s(61));
					if (n = r.__html, n != null) {
						if (i.children != null) throw Error(s(60));
						a?.__html !== n && (e.innerHTML = n);
					}
				}
				break;
			case "multiple":
				e.multiple = r && typeof r != "function" && typeof r != "symbol";
				break;
			case "muted":
				e.muted = r && typeof r != "function" && typeof r != "symbol";
				break;
			case "suppressContentEditableWarning":
			case "suppressHydrationWarning":
			case "defaultValue":
			case "defaultChecked":
			case "innerHTML":
			case "ref": break;
			case "autoFocus": break;
			case "xlinkHref":
				if (r == null || typeof r == "function" || typeof r == "boolean" || typeof r == "symbol") {
					e.removeAttribute("xlink:href");
					break;
				}
				n = wn(r), e.setAttributeNS("http://www.w3.org/1999/xlink", "xlink:href", n);
				break;
			case "contentEditable":
			case "spellCheck":
			case "draggable":
			case "value":
			case "autoReverse":
			case "externalResourcesRequired":
			case "focusable":
			case "preserveAlpha":
				r != null && typeof r != "function" && typeof r != "symbol" ? e.setAttribute(n, r) : e.removeAttribute(n);
				break;
			case "inert":
			case "allowFullScreen":
			case "async":
			case "autoPlay":
			case "controls":
			case "credentialless":
			case "default":
			case "defer":
			case "disabled":
			case "disablePictureInPicture":
			case "disableRemotePlayback":
			case "formNoValidate":
			case "hidden":
			case "loop":
			case "noModule":
			case "noValidate":
			case "open":
			case "playsInline":
			case "readOnly":
			case "required":
			case "reversed":
			case "scoped":
			case "seamless":
			case "itemScope":
				r && typeof r != "function" && typeof r != "symbol" ? e.setAttribute(n, "") : e.removeAttribute(n);
				break;
			case "capture":
			case "download":
				!0 === r ? e.setAttribute(n, "") : !1 !== r && r != null && typeof r != "function" && typeof r != "symbol" ? e.setAttribute(n, r) : e.removeAttribute(n);
				break;
			case "cols":
			case "rows":
			case "size":
			case "span":
				r != null && typeof r != "function" && typeof r != "symbol" && !isNaN(r) && 1 <= r ? e.setAttribute(n, r) : e.removeAttribute(n);
				break;
			case "rowSpan":
			case "start":
				r == null || typeof r == "function" || typeof r == "symbol" || isNaN(r) ? e.removeAttribute(n) : e.setAttribute(n, r);
				break;
			case "popover":
				Q("beforetoggle", e), Q("toggle", e), en(e, "popover", r);
				break;
			case "xlinkActuate":
				nn(e, "http://www.w3.org/1999/xlink", "xlink:actuate", r);
				break;
			case "xlinkArcrole":
				nn(e, "http://www.w3.org/1999/xlink", "xlink:arcrole", r);
				break;
			case "xlinkRole":
				nn(e, "http://www.w3.org/1999/xlink", "xlink:role", r);
				break;
			case "xlinkShow":
				nn(e, "http://www.w3.org/1999/xlink", "xlink:show", r);
				break;
			case "xlinkTitle":
				nn(e, "http://www.w3.org/1999/xlink", "xlink:title", r);
				break;
			case "xlinkType":
				nn(e, "http://www.w3.org/1999/xlink", "xlink:type", r);
				break;
			case "xmlBase":
				nn(e, "http://www.w3.org/XML/1998/namespace", "xml:base", r);
				break;
			case "xmlLang":
				nn(e, "http://www.w3.org/XML/1998/namespace", "xml:lang", r);
				break;
			case "xmlSpace":
				nn(e, "http://www.w3.org/XML/1998/namespace", "xml:space", r);
				break;
			case "is":
				en(e, "is", r);
				break;
			case "innerText":
			case "textContent": return;
			default: if (!(2 < n.length) || n[0] !== "o" && n[0] !== "O" || n[1] !== "n" && n[1] !== "N") n = Sn.get(n) || n, en(e, n, r);
			else return;
		}
		M = !0;
	}
	function tp(e, t, n, r, i, a) {
		switch (n) {
			case "style":
				bn(e, r, a);
				return;
			case "dangerouslySetInnerHTML":
				if (r != null) {
					if (typeof r != "object" || !("__html" in r)) throw Error(s(61));
					if (n = r.__html, n != null) {
						if (i.children != null) throw Error(s(60));
						a?.__html !== n && (e.innerHTML = n);
					}
				}
				break;
			case "children":
				if (typeof r == "string") _n(e, r);
				else if (typeof r == "number" || typeof r == "bigint") _n(e, "" + r);
				else return;
				break;
			case "onScroll":
				r != null && Q("scroll", e);
				return;
			case "onScrollEnd":
				r != null && Q("scrollend", e);
				return;
			case "onClick":
				r != null && (e.onclick = Tn);
				return;
			case "suppressContentEditableWarning":
			case "suppressHydrationWarning":
			case "innerHTML":
			case "ref": return;
			case "innerText":
			case "textContent": return;
			default:
				if (!qt.hasOwnProperty(n)) a: {
					if (n[0] === "o" && n[1] === "n" && (i = n.endsWith("Capture"), a = n.slice(2, i ? n.length - 7 : void 0), t = e[jt] || null, t = t == null ? null : t[n], typeof t == "function" && e.removeEventListener(a, t, i), typeof r == "function")) {
						typeof t != "function" && t !== null && (n in e ? e[n] = null : e.hasAttribute(n) && e.removeAttribute(n)), e.addEventListener(a, r, i);
						break a;
					}
					M = !0, n in e ? e[n] = r : !0 === r ? e.setAttribute(n, "") : en(e, n, r);
				}
				return;
		}
		M = !0;
	}
	function np(e, t, n) {
		switch (t) {
			case "div":
			case "span":
			case "svg":
			case "path":
			case "a":
			case "g":
			case "p":
			case "li": break;
			case "img":
				Q("error", e), Q("load", e);
				var r = !1, i = !1, a;
				for (a in n) if (n.hasOwnProperty(a)) {
					var o = n[a];
					if (o != null) switch (a) {
						case "src":
							r = !0;
							break;
						case "srcSet":
							i = !0;
							break;
						case "children":
						case "dangerouslySetInnerHTML": throw Error(s(137, t));
						default: $(e, t, a, o, n, null);
					}
				}
				i && $(e, t, "srcSet", n.srcSet, n, null), r && $(e, t, "src", n.src, n, null);
				return;
			case "input":
				Q("invalid", e);
				var c = a = o = i = null, l = null, u = null;
				for (r in n) if (n.hasOwnProperty(r)) {
					var d = n[r];
					if (d != null) switch (r) {
						case "name":
							i = d;
							break;
						case "type":
							o = d;
							break;
						case "checked":
							l = d;
							break;
						case "defaultChecked":
							u = d;
							break;
						case "value":
							a = d;
							break;
						case "defaultValue":
							c = d;
							break;
						case "children":
						case "dangerouslySetInnerHTML":
							if (d != null) throw Error(s(137, t));
							break;
						default: $(e, t, r, d, n, null);
					}
				}
				fn(e, a, c, l, u, o, i, !1);
				return;
			case "select":
				for (i in Q("invalid", e), r = o = a = null, n) if (n.hasOwnProperty(i) && (c = n[i], c != null)) switch (i) {
					case "value":
						a = c;
						break;
					case "defaultValue":
						o = c;
						break;
					case "multiple": r = c;
					default: $(e, t, i, c, n, null);
				}
				t = a, n = o, e.multiple = !!r, t == null ? n != null && mn(e, !!r, n, !0) : mn(e, !!r, t, !1);
				return;
			case "textarea":
				for (o in Q("invalid", e), a = i = r = null, n) if (n.hasOwnProperty(o) && (c = n[o], c != null)) switch (o) {
					case "value":
						r = c;
						break;
					case "defaultValue":
						i = c;
						break;
					case "children":
						a = c;
						break;
					case "dangerouslySetInnerHTML":
						if (c != null) throw Error(s(91));
						break;
					default: $(e, t, o, c, n, null);
				}
				gn(e, r, i, a);
				return;
			case "option":
				for (l in n) if (n.hasOwnProperty(l) && (r = n[l], r != null)) switch (l) {
					case "selected":
						e.selected = r && typeof r != "function" && typeof r != "symbol";
						break;
					default: $(e, t, l, r, n, null);
				}
				return;
			case "dialog":
				Q("beforetoggle", e), Q("toggle", e), Q("cancel", e), Q("close", e);
				break;
			case "iframe":
			case "object":
				Q("load", e);
				break;
			case "video":
			case "audio":
				for (r = 0; r < zf.length; r++) Q(zf[r], e);
				break;
			case "image":
				Q("error", e), Q("load", e);
				break;
			case "details":
				Q("toggle", e);
				break;
			case "embed":
			case "source":
			case "link": Q("error", e), Q("load", e);
			case "area":
			case "base":
			case "br":
			case "col":
			case "hr":
			case "keygen":
			case "meta":
			case "param":
			case "track":
			case "wbr":
			case "menuitem":
				for (u in n) if (n.hasOwnProperty(u) && (r = n[u], r != null)) switch (u) {
					case "children":
					case "dangerouslySetInnerHTML": throw Error(s(137, t));
					default: $(e, t, u, r, n, null);
				}
				return;
			default: if (xn(t)) {
				for (d in n) n.hasOwnProperty(d) && (r = n[d], r !== void 0 && tp(e, t, d, r, n, void 0));
				return;
			}
		}
		for (c in n) n.hasOwnProperty(c) && (r = n[c], r != null && $(e, t, c, r, n, null));
	}
	var rp = {};
	function ip(e, t, n, r) {
		switch (t) {
			case "div":
			case "span":
			case "svg":
			case "path":
			case "a":
			case "g":
			case "p":
			case "li": break;
			case "input":
				var i = null, a = null, o = null, c = null, l = null, u = null, d = null;
				for (m in n) {
					var f = n[m];
					if (n.hasOwnProperty(m) && f != null) switch (m) {
						case "checked": break;
						case "value": break;
						case "defaultValue": l = f;
						default: r.hasOwnProperty(m) || $(e, t, m, null, r, f);
					}
				}
				for (var p in r) {
					var m = r[p];
					if (f = n[p], r.hasOwnProperty(p) && (m != null || f != null)) switch (p) {
						case "type":
							m !== f && (M = !0), a = m;
							break;
						case "name":
							m !== f && (M = !0), i = m;
							break;
						case "checked":
							m !== f && (M = !0), u = m;
							break;
						case "defaultChecked":
							m !== f && (M = !0), d = m;
							break;
						case "value":
							m !== f && (M = !0), o = m;
							break;
						case "defaultValue":
							m !== f && (M = !0), c = m;
							break;
						case "children":
						case "dangerouslySetInnerHTML":
							if (m != null) throw Error(s(137, t));
							break;
						default: m !== f && $(e, t, p, m, r, f);
					}
				}
				dn(e, o, c, l, u, d, a, i);
				return;
			case "select":
				for (a in m = o = c = p = null, n) if (l = n[a], n.hasOwnProperty(a) && l != null) switch (a) {
					case "value": break;
					case "multiple": m = l;
					default: r.hasOwnProperty(a) || $(e, t, a, null, r, l);
				}
				for (i in r) if (a = r[i], l = n[i], r.hasOwnProperty(i) && (a != null || l != null)) switch (i) {
					case "value":
						a !== l && (M = !0), p = a;
						break;
					case "defaultValue":
						a !== l && (M = !0), c = a;
						break;
					case "multiple": a !== l && (M = !0), o = a;
					default: a !== l && $(e, t, i, a, r, l);
				}
				t = c, n = o, r = m, p == null ? !!r != !!n && (t == null ? mn(e, !!n, n ? [] : "", !1) : mn(e, !!n, t, !0)) : mn(e, !!n, p, !1);
				return;
			case "textarea":
				for (c in m = p = null, n) if (i = n[c], n.hasOwnProperty(c) && i != null && !r.hasOwnProperty(c)) switch (c) {
					case "value": break;
					case "children": break;
					default: $(e, t, c, null, r, i);
				}
				for (o in r) if (i = r[o], a = n[o], r.hasOwnProperty(o) && (i != null || a != null)) switch (o) {
					case "value":
						i !== a && (M = !0), p = i;
						break;
					case "defaultValue":
						i !== a && (M = !0), m = i;
						break;
					case "children": break;
					case "dangerouslySetInnerHTML":
						if (i != null) throw Error(s(91));
						break;
					default: i !== a && $(e, t, o, i, r, a);
				}
				hn(e, p, m);
				return;
			case "option":
				for (var h in n) if (p = n[h], n.hasOwnProperty(h) && p != null && !r.hasOwnProperty(h)) switch (h) {
					case "selected":
						e.selected = !1;
						break;
					default: $(e, t, h, null, r, p);
				}
				for (l in r) if (p = r[l], m = n[l], r.hasOwnProperty(l) && p !== m && (p != null || m != null)) switch (l) {
					case "selected":
						p !== m && (M = !0), e.selected = p && typeof p != "function" && typeof p != "symbol";
						break;
					default: $(e, t, l, p, r, m);
				}
				return;
			case "img":
			case "link":
			case "area":
			case "base":
			case "br":
			case "col":
			case "embed":
			case "hr":
			case "keygen":
			case "meta":
			case "param":
			case "source":
			case "track":
			case "wbr":
			case "menuitem":
				for (var g in n) p = n[g], n.hasOwnProperty(g) && p != null && !r.hasOwnProperty(g) && $(e, t, g, null, r, p);
				for (u in r) if (p = r[u], m = n[u], r.hasOwnProperty(u) && p !== m && (p != null || m != null)) switch (u) {
					case "children":
					case "dangerouslySetInnerHTML":
						if (p != null) throw Error(s(137, t));
						break;
					default: $(e, t, u, p, r, m);
				}
				return;
			default: if (xn(t)) {
				for (var _ in n) p = n[_], n.hasOwnProperty(_) && p !== void 0 && !r.hasOwnProperty(_) && tp(e, t, _, void 0, r, p);
				for (d in r) p = r[d], m = n[d], !r.hasOwnProperty(d) || p === m || p === void 0 && m === void 0 || tp(e, t, d, p, r, m);
				return;
			}
		}
		for (var v in n) p = n[v], n.hasOwnProperty(v) && p != null && !r.hasOwnProperty(v) && $(e, t, v, null, r, p);
		for (f in r) p = r[f], m = n[f], !r.hasOwnProperty(f) || p === m || p == null && m == null || $(e, t, f, p, r, m);
	}
	function ap(e) {
		switch (e) {
			case "css":
			case "script":
			case "font":
			case "img":
			case "image":
			case "input":
			case "link": return !0;
			default: return !1;
		}
	}
	function op() {
		if (typeof performance.getEntriesByType == "function") {
			for (var e = 0, t = 0, n = performance.getEntriesByType("resource"), r = 0; r < n.length; r++) {
				var i = n[r], a = i.transferSize, o = i.initiatorType, s = i.duration;
				if (a && s && ap(o)) {
					for (o = 0, s = i.responseEnd, r += 1; r < n.length; r++) {
						var c = n[r], l = c.startTime;
						if (l > s) break;
						var u = c.transferSize, d = c.initiatorType;
						u && ap(d) && (c = c.responseEnd, o += u * (c < s ? 1 : (s - l) / (c - l)));
					}
					if (--r, t += 8 * (a + o) / (i.duration / 1e3), e++, 10 < e) break;
				}
			}
			if (0 < e) return t / e / 1e6;
		}
		return navigator.connection && (e = navigator.connection.downlink, typeof e == "number") ? e : 5;
	}
	var sp = null, cp = null;
	function lp(e) {
		return e.nodeType === 9 ? e : e.ownerDocument;
	}
	function up(e) {
		switch (e) {
			case "http://www.w3.org/2000/svg": return 1;
			case "http://www.w3.org/1998/Math/MathML": return 2;
			default: return 0;
		}
	}
	function dp(e, t) {
		if (e === 0) switch (t) {
			case "svg": return 1;
			case "math": return 2;
			default: return 0;
		}
		return e === 1 && t === "foreignObject" ? 0 : e;
	}
	function fp(e, t, n, r) {
		return n = lp(n).createElement(e), n[At] = r, n[jt] = t, np(n, e, t), Wt(n), n;
	}
	function pp(e, t) {
		return e === "textarea" || e === "noscript" || typeof t.children == "string" || typeof t.children == "number" || typeof t.children == "bigint" || typeof t.dangerouslySetInnerHTML == "object" && t.dangerouslySetInnerHTML !== null && t.dangerouslySetInnerHTML.__html != null;
	}
	var mp = null;
	function hp() {
		var e = window.event;
		return e && e.type === "popstate" ? e !== mp && (mp = e, !0) : (mp = null, !1);
	}
	var gp = typeof setTimeout == "function" ? setTimeout : void 0, _p = typeof clearTimeout == "function" ? clearTimeout : void 0, vp = typeof Promise == "function" ? Promise : void 0, yp = typeof requestAnimationFrame == "function" ? requestAnimationFrame : gp, bp = typeof queueMicrotask == "function" ? queueMicrotask : vp === void 0 ? gp : function(e) {
		return vp.resolve(null).then(e).catch(xp);
	};
	function xp(e) {
		setTimeout(function() {
			throw e;
		});
	}
	function Sp(e) {
		return e === "head";
	}
	function Cp(e, t) {
		var n = t, r = 0;
		do {
			var i = n.nextSibling;
			if (e.removeChild(n), i && i.nodeType === 8) {
				if (n = i.data, n === "/$" || n === "/&") {
					if (r === 0) {
						e.removeChild(i), Hh(t);
						return;
					}
					r--;
				} else if (n === "$" || n === "$?" || n === "$~" || n === "$!" || n === "&") r++;
				else if (n === "html") _m(e.ownerDocument.documentElement);
				else if (n === "head") {
					n = e.ownerDocument.head, _m(n);
					for (var a = n.firstChild; a;) {
						var o = a.nextSibling, s = a.nodeName;
						a[Lt] || s === "SCRIPT" || s === "STYLE" || s === "LINK" && a.rel.toLowerCase() === "stylesheet" || n.removeChild(a), a = o;
					}
				} else n === "body" && _m(e.ownerDocument.body);
			}
			n = i;
		} while (n);
		Hh(t);
	}
	function wp(e, t) {
		var n = e;
		e = 0;
		do {
			var r = n.nextSibling;
			if (n.nodeType === 1 ? t ? (n._stashedDisplay = n.style.display, n.style.display = "none") : (n.style.display = n._stashedDisplay || "", n.getAttribute("style") === "" && n.removeAttribute("style")) : n.nodeType === 3 && (t ? (n._stashedText = n.nodeValue, n.nodeValue = "") : n.nodeValue = n._stashedText || ""), r && r.nodeType === 8) {
				if (n = r.data, n === "/$") {
					if (e === 0) break;
					e--;
				} else n !== "$" && n !== "$?" && n !== "$~" && n !== "$!" || e++;
			}
			n = r;
		} while (n);
	}
	function Tp(e, t, n) {
		if (t = CSS.escape(t) === t ? t : "r-" + btoa(t).replace(/=/g, ""), e.style.viewTransitionName = t, n != null && (e.style.viewTransitionClass = n), n = getComputedStyle(e), n.display === "inline") {
			if (t = e.getClientRects(), t.length === 1) var r = 1;
			else for (var i = r = 0; i < t.length; i++) {
				var a = t[i];
				0 < a.width && 0 < a.height && r++;
			}
			r === 1 && (e = e.style, e.display = t.length === 1 ? "inline-block" : "block", e.marginTop = "-" + n.paddingTop, e.marginBottom = "-" + n.paddingBottom);
		}
	}
	function Ep(e, t) {
		e = e.style, t = t.style;
		var n = t == null ? null : t.hasOwnProperty("viewTransitionName") ? t.viewTransitionName : t.hasOwnProperty("view-transition-name") ? t["view-transition-name"] : null;
		e.viewTransitionName = n == null || typeof n == "boolean" ? "" : ("" + n).trim(), n = t == null ? null : t.hasOwnProperty("viewTransitionClass") ? t.viewTransitionClass : t.hasOwnProperty("view-transition-class") ? t["view-transition-class"] : null, e.viewTransitionClass = n == null || typeof n == "boolean" ? "" : ("" + n).trim(), e.display === "inline-block" && (t == null ? e.display = e.margin = "" : (n = t.display, e.display = n == null || typeof n == "boolean" ? "" : n, n = t.margin, n == null ? (n = t.hasOwnProperty("marginTop") ? t.marginTop : t["margin-top"], e.marginTop = n == null || typeof n == "boolean" ? "" : n, t = t.hasOwnProperty("marginBottom") ? t.marginBottom : t["margin-bottom"], e.marginBottom = t == null || typeof t == "boolean" ? "" : t) : e.margin = n));
	}
	function Dp(e, t, n) {
		return n = n.ownerDocument.defaultView, {
			rect: e,
			abs: t.position === "absolute" || t.position === "fixed",
			clip: t.clipPath !== "none" || t.overflow !== "visible" || t.filter !== "none" || t.mask !== "none" || t.mask !== "none" || t.borderRadius !== "0px",
			view: 0 <= e.bottom && 0 <= e.right && e.top <= n.innerHeight && e.left <= n.innerWidth
		};
	}
	function Op(e) {
		return Dp(e.getBoundingClientRect(), getComputedStyle(e), e);
	}
	function kp(e) {
		var t = e.getBoundingClientRect();
		t = new DOMRect(t.x + 2e4, t.y + 2e4, t.width, t.height);
		var n = getComputedStyle(e);
		return Dp(t, n, e);
	}
	function Ap(e) {
		return e.documentElement.clientHeight;
	}
	function jp(e) {
		this.addEventListener("load", e), this.addEventListener("error", e);
	}
	function Mp(e, t, n, r, i, a, o, s, c) {
		var l = t.nodeType === 9 ? t : t.ownerDocument;
		try {
			var u = l.startViewTransition({
				update: function() {
					var t = l.defaultView, n = t.navigation && t.navigation.transition, o = l.fonts.status;
					r();
					var s = [];
					if (o === "loaded" && (Ap(l), l.fonts.status === "loading" && s.push(l.fonts.ready)), o = s.length, e !== null) for (var c = e.suspenseyImages, u = 0, d = 0; d < c.length; d++) {
						var f = c[d];
						if (!f.complete) {
							var p = f.getBoundingClientRect();
							if (0 < p.bottom && 0 < p.right && p.top < t.innerHeight && p.left < t.innerWidth) {
								if (u += Xm(f), u > $m) {
									s.length = o;
									break;
								}
								f = new Promise(jp.bind(f)), s.push(f);
							}
						}
					}
					if (0 < s.length) return t = Promise.race([Promise.all(s), new Promise(function(e) {
						return setTimeout(e, 500);
					})]).then(i, i), (n ? Promise.allSettled([n.finished, t]) : t).then(a, a);
					if (i(), n) return n.finished.then(a, a);
					a();
				},
				types: n
			});
			l.__reactViewTransition = u;
			var d = [];
			return u.ready.then(function() {
				for (var e = l.documentElement.getAnimations({ subtree: !0 }), t = 0; t < e.length; t++) {
					var n = e[t], r = n.effect, i = r.pseudoElement;
					if (i != null && i.startsWith("::view-transition")) {
						d.push(n), n = r.getKeyframes();
						for (var a = i = void 0, s = !0, c = 0; c < n.length; c++) {
							var u = n[c], f = u.width;
							if (i === void 0) i = f;
							else if (i !== f) {
								s = !1;
								break;
							}
							if (f = u.height, a === void 0) a = f;
							else if (a !== f) {
								s = !1;
								break;
							}
							delete u.width, delete u.height, u.transform === "none" && delete u.transform;
						}
						s && i !== void 0 && a !== void 0 && (r.setKeyframes(n), s = getComputedStyle(r.target, r.pseudoElement), s.width !== i || s.height !== a) && (s = n[0], s.width = i, s.height = a, s = n[n.length - 1], s.width = i, s.height = a, r.setKeyframes(n));
					}
				}
				o();
			}, function(e) {
				l.__reactViewTransition === u && (l.__reactViewTransition = null);
				try {
					if (typeof e == "object" && e) switch (e.name) {
						case "InvalidStateError": (e.message === "View transition was skipped because document visibility state is hidden." || e.message === "Skipping view transition because document visibility state has become hidden." || e.message === "Skipping view transition because viewport size changed." || e.message === "Transition was aborted because of invalid state") && (e = null);
					}
					e !== null && c(e);
				} finally {
					r(), i(), o();
				}
			}), u.finished.finally(function() {
				for (var e = 0; e < d.length; e++) d[e].cancel();
				l.__reactViewTransition === u && (l.__reactViewTransition = null), s();
			}), u;
		} catch {
			return r(), i(), o(), null;
		}
	}
	function Np(e, t) {
		this._scope = document.documentElement, this._selector = "::view-transition-" + e + "(" + t + ")";
	}
	Np.prototype.animate = function(e, t) {
		return t = typeof t == "number" ? { duration: t } : T({}, t), t.pseudoElement = this._selector, this._scope.animate(e, t);
	}, Np.prototype.getAnimations = function() {
		for (var e = this._scope, t = this._selector, n = e.getAnimations({ subtree: !0 }), r = [], i = 0; i < n.length; i++) {
			var a = n[i].effect;
			a !== null && a.target === e && a.pseudoElement === t && r.push(n[i]);
		}
		return r;
	}, Np.prototype.getComputedStyle = function() {
		return getComputedStyle(this._scope, this._selector);
	};
	function Pp(e) {
		return {
			name: e,
			group: new Np("group", e),
			imagePair: new Np("image-pair", e),
			old: new Np("old", e),
			new: new Np("new", e)
		};
	}
	function Fp(e) {
		this._fragmentFiber = e, this._observers = this._eventListeners = null;
	}
	Fp.prototype.addEventListener = function(e, t, n) {
		var r = null, i = null;
		if (!(n != null && typeof n != "boolean" && (r = n.signal || null, r !== null && r.aborted))) {
			this._eventListeners === null && (this._eventListeners = []);
			var a = this._eventListeners;
			if (Bp(a, e, t, n) === -1) {
				var o = this, s = t;
				n != null && typeof n != "boolean" && !0 === n.once && (s = function(r) {
					o.removeEventListener(e, t, n), typeof t == "function" ? t.call(this, r) : t.handleEvent(r);
				}), r !== null && (i = o.removeEventListener.bind(o, e, t, n), r.addEventListener("abort", i, { once: !0 }), i = r.removeEventListener.bind(r, "abort", i)), r = Rp(n), a.push({
					type: e,
					listener: t,
					optionsOrUseCapture: n,
					attachedListener: s,
					cleanup: i
				}), h(this._fragmentFiber.child, !1, Ip, e, s, r);
			}
			this._eventListeners = a;
		}
	};
	function Ip(e, t, n, r) {
		return b(e).addEventListener(t, n, r), !1;
	}
	Fp.prototype.removeEventListener = function(e, t, n) {
		var r = this._eventListeners;
		if (r !== null && (t = Bp(r, e, t, n), t !== -1)) {
			var i = r[t];
			n = i.attachedListener;
			var a = i.cleanup;
			i = Rp(i.optionsOrUseCapture), h(this._fragmentFiber.child, !1, Lp, e, n, i), r.splice(t, 1), a !== null && a();
		}
	};
	function Lp(e, t, n, r) {
		return b(e).removeEventListener(t, n, r), !1;
	}
	function Rp(e) {
		return e != null && typeof e != "boolean" && (!0 === e.once || e.signal instanceof AbortSignal) ? {
			capture: e.capture,
			passive: e.passive
		} : e;
	}
	function zp(e) {
		return e == null ? "c=0" : typeof e == "boolean" ? "c=" + (e ? "1" : "0") : "c=" + (e.capture ? "1" : "0");
	}
	function Bp(e, t, n, r) {
		if (e.length === 0) return -1;
		r = zp(r);
		for (var i = 0; i < e.length; i++) {
			var a = e[i];
			if (a.type === t && a.listener === n && zp(a.optionsOrUseCapture) === r) return i;
		}
		return -1;
	}
	Fp.prototype.dispatchEvent = function(e) {
		var t = g(this._fragmentFiber);
		if (t === null) return !0;
		t = b(t);
		var n = this._eventListeners;
		if (n !== null && 0 < n.length || !e.bubbles) {
			var r = t.nodeType === 9 ? t.createComment("") : document.createTextNode("");
			if (n) for (var i = 0; i < n.length; i++) {
				var a = n[i];
				r.addEventListener(a.type, a.attachedListener, Rp(a.optionsOrUseCapture));
			}
			if (t.appendChild(r), e = r.dispatchEvent(e), n) for (i = 0; i < n.length; i++) a = n[i], r.removeEventListener(a.type, a.attachedListener, Rp(a.optionsOrUseCapture));
			return t.removeChild(r), e;
		}
		return t.dispatchEvent(e);
	}, Fp.prototype.focus = function(e) {
		h(this._fragmentFiber.child, !0, Vp, e, void 0, void 0);
	};
	function Vp(e, t) {
		return e.tag !== 6 && (e = b(e), pm(e, t));
	}
	Fp.prototype.focusLast = function(e) {
		var t = [];
		h(this._fragmentFiber.child, !0, Hp, t, void 0, void 0);
		for (var n = t.length - 1; 0 <= n && !Vp(t[n], e); n--);
	};
	function Hp(e, t) {
		return t.push(e), !1;
	}
	Fp.prototype.blur = function() {
		var e = g(this._fragmentFiber);
		e !== null && (e = b(e), e = lp(e).activeElement, e !== null && h(this._fragmentFiber.child, !1, Up, e, void 0, void 0));
	};
	function Up(e, t) {
		return e.tag !== 6 && (e = b(e), e === t || e.contains(t) ? (t.blur(), !0) : !1);
	}
	Fp.prototype.observeUsing = function(e) {
		this._observers === null && (this._observers = /* @__PURE__ */ new Set()), this._observers.add(e), h(this._fragmentFiber.child, !1, Wp, e, void 0, void 0);
	};
	function Wp(e, t) {
		return e.tag !== 6 && (e = b(e), t.observe(e), !1);
	}
	Fp.prototype.unobserveUsing = function(e) {
		var t = this._observers;
		if (t !== null && t.has(e)) {
			t.delete(e), h(this._fragmentFiber.child, !1, Gp, e, void 0, void 0);
			for (var n = t = 0; n < Kp.length; n++) {
				var r = Kp[n];
				r.fragmentInstance === this && r.observer === e ? e.unobserve(r.instance) : Kp[t++] = r;
			}
			Kp.length = t;
		}
	};
	function Gp(e, t) {
		return e.tag !== 6 && (e = b(e), t.unobserve(e), !1);
	}
	var Kp = [], qp = !1;
	function Jp(e, t, n) {
		Kp.push({
			fragmentInstance: e,
			observer: t,
			instance: n
		}), qp || (qp = !0, mm(function() {
			qp = !1;
			var e = Kp;
			Kp = [];
			for (var t = 0; t < e.length; t++) {
				var n = e[t];
				n.observer.unobserve(n.instance);
			}
		}));
	}
	Fp.prototype.getClientRects = function() {
		var e = [];
		return h(this._fragmentFiber.child, !1, Yp, e, void 0, void 0), e;
	};
	function Yp(e, t) {
		if (e.tag === 6) {
			e = e.stateNode;
			var n = e.ownerDocument.createRange();
			n.selectNodeContents(e), t.push.apply(t, n.getClientRects());
		} else e = b(e), t.push.apply(t, e.getClientRects());
		return !1;
	}
	Fp.prototype.getRootNode = function(e) {
		var t = g(this._fragmentFiber);
		return t === null ? this : b(t).getRootNode(e);
	}, Fp.prototype.compareDocumentPosition = function(e) {
		var t = g(this._fragmentFiber);
		if (t === null) return Node.DOCUMENT_POSITION_DISCONNECTED;
		var n = [];
		h(this._fragmentFiber.child, !1, Hp, n, void 0, void 0);
		var r = b(t);
		if (n.length === 0) {
			if (n = r, _(this._fragmentFiber)) {
				a: {
					for (t = this._fragmentFiber.return; t !== null;) {
						if (t.tag === 4) {
							t = t.stateNode.containerInfo;
							break a;
						}
						if (t.tag === 3 || t.tag === 5 || t.tag === 27) break;
						t = t.return;
					}
					t = null;
				}
				t != null && (n = t);
			}
			t = this._fragmentFiber;
			var i = r = n.compareDocumentPosition(e);
			return n === e ? i = Node.DOCUMENT_POSITION_CONTAINS : r & Node.DOCUMENT_POSITION_CONTAINED_BY && (n = v(t)[1], n === null ? i = Node.DOCUMENT_POSITION_PRECEDING : (e = b(n).compareDocumentPosition(e), i = e === 0 || e & Node.DOCUMENT_POSITION_FOLLOWING ? Node.DOCUMENT_POSITION_FOLLOWING : Node.DOCUMENT_POSITION_PRECEDING)), i |= Node.DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC;
		}
		t = b(n[0]), i = b(n[n.length - 1]);
		var a = _(this._fragmentFiber) ? t.parentElement : r;
		if (a == null) return Node.DOCUMENT_POSITION_DISCONNECTED;
		r = a.compareDocumentPosition(t) & Node.DOCUMENT_POSITION_CONTAINED_BY, a = a.compareDocumentPosition(i) & Node.DOCUMENT_POSITION_CONTAINED_BY;
		var o = t.compareDocumentPosition(e), s = i.compareDocumentPosition(e), c = o & Node.DOCUMENT_POSITION_CONTAINED_BY || s & Node.DOCUMENT_POSITION_CONTAINED_BY;
		return s = r && a && o & Node.DOCUMENT_POSITION_FOLLOWING && s & Node.DOCUMENT_POSITION_PRECEDING, t = r && t === e || a && i === e || c || s ? Node.DOCUMENT_POSITION_CONTAINED_BY : !r && t === e || !a && i === e ? Node.DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC : o, t & Node.DOCUMENT_POSITION_DISCONNECTED || t & Node.DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC || Xp(t, this._fragmentFiber, n[0], n[n.length - 1], e) ? t : Node.DOCUMENT_POSITION_IMPLEMENTATION_SPECIFIC;
	};
	function Xp(e, t, n, r, i) {
		var a = Bt(i);
		if (e & Node.DOCUMENT_POSITION_CONTAINED_BY) {
			if (n = !!a) a: {
				for (; a !== null;) {
					if (a.tag === 7 && (a === t || a.alternate === t)) {
						n = !0;
						break a;
					}
					a = a.return;
				}
				n = !1;
			}
			return n;
		}
		if (e & Node.DOCUMENT_POSITION_CONTAINS) {
			if (a === null) return a = i.ownerDocument, i === a || i === a.documentElement || i === a.body;
			a: {
				for (a = t, t = g(t); a !== null;) {
					if (!(a.tag !== 5 && a.tag !== 3 && a.tag !== 27 || a !== t && a.alternate !== t)) {
						a = !0;
						break a;
					}
					a = a.return;
				}
				a = !1;
			}
			return a;
		}
		return e & Node.DOCUMENT_POSITION_PRECEDING ? ((t = !!a) && !(t = a === n) && (t = te(n, a, w), t === null ? t = !1 : (h(t, !0, ee, a, n), a = x, x = null, t = a !== null)), t) : e & Node.DOCUMENT_POSITION_FOLLOWING ? ((t = !!a) && !(t = a === r) && (t = te(r, a, w), t === null ? t = !1 : (h(t, !0, C, a, r), a = x, S = x = null, t = a !== null)), t) : !1;
	}
	function Zp(e, t) {
		var n = e.ownerDocument.createRange();
		n.selectNodeContents(e), e = n.getBoundingClientRect(), window.scrollTo(window.scrollX + e.left, t ? window.scrollY + e.top : window.scrollY + e.bottom - window.innerHeight);
	}
	Fp.prototype.scrollIntoView = function(e) {
		if (typeof e == "object") throw Error(s(566));
		var t = [];
		h(this._fragmentFiber.child, !1, Hp, t, void 0, void 0);
		var n = !1 !== e;
		if (t.length === 0) {
			var r = v(this._fragmentFiber);
			if (r = n ? r[1] || r[0] || g(this._fragmentFiber) : r[0] || r[1], r === null) return;
			if (r.tag === 6) {
				e = b(r), Zp(e, n);
				return;
			}
			if (r = b(r), r.nodeType !== 9) {
				if (r.nodeType === 11) {
					n = "host" in r ? r.host : null, n !== null && n.scrollIntoView(e);
					return;
				}
				r.scrollIntoView(e);
			}
		}
		for (r = n ? t.length - 1 : 0; r !== (n ? -1 : t.length);) {
			var i = t[r];
			i.tag === 6 ? (i = b(i), Zp(i, n)) : b(i).scrollIntoView(e), r += n ? -1 : 1;
		}
	};
	function Qp(e, t) {
		return e = b(e), $p(e, t), !1;
	}
	function $p(e, t) {
		e.reactFragments ??= /* @__PURE__ */ new Set(), e.reactFragments.add(t);
	}
	function em(e, t) {
		var n = t._eventListeners;
		if (n !== null) for (var r = 0; r < n.length; r++) {
			var i = n[r];
			e.addEventListener(i.type, i.attachedListener, Rp(i.optionsOrUseCapture));
		}
		e.nodeType !== 3 && (n = t._observers, n !== null && n.forEach(function(n) {
			for (var r = 0, i = 0; i < Kp.length; i++) {
				var a = Kp[i];
				(a.fragmentInstance !== t || a.observer !== n || a.instance !== e) && (Kp[r++] = a);
			}
			Kp.length = r, n.observe(e);
		}), $p(e, t));
	}
	function tm(e, t) {
		var n = t._eventListeners;
		if (n !== null) for (var r = 0; r < n.length; r++) {
			var i = n[r];
			e.removeEventListener(i.type, i.attachedListener, Rp(i.optionsOrUseCapture));
		}
		e.nodeType !== 3 && (n = t._observers, n !== null && n.forEach(function(n) {
			typeof n.rootMargin == "string" ? Jp(t, n, e) : n.unobserve(e);
		}), e.reactFragments != null && e.reactFragments.delete(t));
	}
	function nm(e) {
		var t = e.firstChild;
		for (t && t.nodeType === 10 && (t = t.nextSibling); t;) {
			var n = t;
			switch (t = t.nextSibling, n.nodeName) {
				case "HTML":
				case "HEAD":
				case "BODY":
					nm(n), zt(n);
					continue;
				case "SCRIPT":
				case "STYLE": continue;
				case "LINK": if (n.rel.toLowerCase() === "stylesheet") continue;
			}
			e.removeChild(n);
		}
	}
	function rm(e, t, n, r) {
		for (; e.nodeType === 1;) {
			var i = n;
			if (e.nodeName.toLowerCase() !== t.toLowerCase()) {
				if (!r && (e.nodeName !== "INPUT" || e.type !== "hidden")) break;
			} else if (!r) {
				if (t === "input" && e.type === "hidden") {
					var a = i.name == null ? null : "" + i.name;
					if (i.type === "hidden" && e.getAttribute("name") === a) return e;
				} else return e;
			} else if (!e[Lt]) switch (t) {
				case "meta":
					if (!e.hasAttribute("itemprop")) break;
					return e;
				case "link":
					if (a = e.getAttribute("rel"), a === "stylesheet" && e.hasAttribute("data-precedence") || a !== i.rel || e.getAttribute("href") !== (i.href == null || i.href === "" ? null : i.href) || e.getAttribute("crossorigin") !== (i.crossOrigin == null ? null : i.crossOrigin) || e.getAttribute("title") !== (i.title == null ? null : i.title)) break;
					return e;
				case "style":
					if (e.hasAttribute("data-precedence")) break;
					return e;
				case "script":
					if (a = e.getAttribute("src"), (a !== (i.src == null ? null : i.src) || e.getAttribute("type") !== (i.type == null ? null : i.type) || e.getAttribute("crossorigin") !== (i.crossOrigin == null ? null : i.crossOrigin)) && a && e.hasAttribute("async") && !e.hasAttribute("itemprop")) break;
					return e;
				default: return e;
			}
			if (e = lm(e.nextSibling), e === null) break;
		}
		return null;
	}
	function im(e, t, n) {
		if (t === "") return null;
		for (; e.nodeType !== 3;) if ((e.nodeType !== 1 || e.nodeName !== "INPUT" || e.type !== "hidden") && !n || (e = lm(e.nextSibling), e === null)) return null;
		return e;
	}
	function am(e, t) {
		for (; e.nodeType !== 8;) if ((e.nodeType !== 1 || e.nodeName !== "INPUT" || e.type !== "hidden") && !t || (e = lm(e.nextSibling), e === null)) return null;
		return e;
	}
	function om(e) {
		return e.data === "$?" || e.data === "$~";
	}
	function sm(e) {
		return e.data === "$!" || e.data === "$?" && e.ownerDocument.readyState !== "loading";
	}
	function cm(e, t) {
		var n = e.ownerDocument;
		if (e.data === "$~") e._reactRetry = t;
		else if (e.data !== "$?" || n.readyState !== "loading") t();
		else {
			var r = function() {
				t(), n.removeEventListener("DOMContentLoaded", r);
			};
			n.addEventListener("DOMContentLoaded", r), e._reactRetry = r;
		}
	}
	function lm(e) {
		for (; e != null; e = e.nextSibling) {
			var t = e.nodeType;
			if (t === 1 || t === 3) break;
			if (t === 8) {
				if (t = e.data, t === "$" || t === "$!" || t === "$?" || t === "$~" || t === "&" || t === "F!" || t === "F") break;
				if (t === "/$" || t === "/&") return null;
			}
		}
		return e;
	}
	var um = null;
	function dm(e) {
		e = e.nextSibling;
		for (var t = 0; e;) {
			if (e.nodeType === 8) {
				var n = e.data;
				if (n === "/$" || n === "/&") {
					if (t === 0) return lm(e.nextSibling);
					t--;
				} else n !== "$" && n !== "$!" && n !== "$?" && n !== "$~" && n !== "&" || t++;
			}
			e = e.nextSibling;
		}
		return null;
	}
	function fm(e) {
		e = e.previousSibling;
		for (var t = 0; e;) {
			if (e.nodeType === 8) {
				var n = e.data;
				if (n === "$" || n === "$!" || n === "$?" || n === "$~" || n === "&") {
					if (t === 0) return e;
					t--;
				} else n !== "/$" && n !== "/&" || t++;
			}
			e = e.previousSibling;
		}
		return null;
	}
	function pm(e, t) {
		function n() {
			r = !0;
		}
		if (e.ownerDocument.activeElement === e) return !0;
		var r = !1;
		try {
			e.ownerDocument.addEventListener("focus", n, !0), (e.focus || HTMLElement.prototype.focus).call(e, t);
		} finally {
			e.ownerDocument.removeEventListener("focus", n, !0);
		}
		return r;
	}
	function mm(e) {
		yp(function() {
			yp(function(t) {
				return e(t);
			});
		});
	}
	function hm(e, t, n) {
		switch (t = lp(n), e) {
			case "html":
				if (e = t.documentElement, !e) throw Error(s(452));
				return e;
			case "head":
				if (e = t.head, !e) throw Error(s(453));
				return e;
			case "body":
				if (e = t.body, !e) throw Error(s(454));
				return e;
			default: throw Error(s(451));
		}
	}
	function gm(e, t, n) {
		for (var r in n) {
			var i = n[r];
			n.hasOwnProperty(r) && i != null && $(e, t, r, null, rp, i);
		}
		n.dangerouslySetInnerHTML != null && (e.textContent = ""), e.onclick === Tn && (e.onclick = null), zt(e);
	}
	function _m(e) {
		for (var t = e.attributes; t.length;) e.removeAttributeNode(t[0]);
		zt(e);
	}
	var vm = /* @__PURE__ */ new Map(), ym = /* @__PURE__ */ new Set();
	function bm(e) {
		if (typeof e.getRootNode == "function") {
			var t = e.getRootNode();
			if (t.nodeType === 9 || t.nodeType === 11) return t;
		}
		return e.nodeType === 9 ? e : e.ownerDocument;
	}
	var xm = k.d;
	k.d = {
		f: Sm,
		r: Cm,
		D: Em,
		C: Dm,
		L: Om,
		m: km,
		X: jm,
		S: Am,
		M: Mm
	};
	function Sm() {
		var e = xm.f(), t = zd();
		return e || t;
	}
	function Cm(e) {
		var t = Vt(e);
		t !== null && t.tag === 5 && t.type === "form" ? ic(t) : xm.r(e);
	}
	var wm = typeof document > "u" ? null : document;
	function Tm(e, t, n) {
		var r = wm;
		if (r && typeof t == "string" && t) {
			var i = un(t);
			i = "link[rel=\"" + e + "\"][href=\"" + i + "\"]", typeof n == "string" && (i += "[crossorigin=\"" + n + "\"]"), ym.has(i) || (ym.add(i), e = {
				rel: e,
				crossOrigin: n,
				href: t
			}, r.querySelector(i) === null && (t = r.createElement("link"), np(t, "link", e), Wt(t), r.head.appendChild(t)));
		}
	}
	function Em(e) {
		xm.D(e), Tm("dns-prefetch", e, null);
	}
	function Dm(e, t) {
		xm.C(e, t), Tm("preconnect", e, t);
	}
	function Om(e, t, n) {
		xm.L(e, t, n);
		var r = wm;
		if (r && e && t) {
			var i = "link[rel=\"preload\"][as=\"" + un(t) + "\"]";
			t === "image" && n && n.imageSrcSet ? (i += "[imagesrcset=\"" + un(n.imageSrcSet) + "\"]", typeof n.imageSizes == "string" && (i += "[imagesizes=\"" + un(n.imageSizes) + "\"]")) : i += "[href=\"" + un(e) + "\"]";
			var a = i;
			switch (t) {
				case "style":
					a = Pm(e);
					break;
				case "script": a = Rm(e);
			}
			if (!(vm.has(a) || (e = T({
				rel: "preload",
				href: t === "image" && n && n.imageSrcSet ? void 0 : e,
				as: t
			}, n), vm.set(a, e), r.querySelector(i) !== null || t === "style" && r.querySelector(Fm(a)) || t === "script" && r.querySelector(zm(a))))) {
				var o = r.createElement("link");
				np(o, "link", e), t === "style" && (o[Rt] = !0, o.onload = o.onerror = function() {
					Gt(o);
				}), Wt(o), r.head.appendChild(o);
			}
		}
	}
	function km(e, t) {
		xm.m(e, t);
		var n = wm;
		if (n && e) {
			var r = t && typeof t.as == "string" ? t.as : "script", i = "link[rel=\"modulepreload\"][as=\"" + un(r) + "\"][href=\"" + un(e) + "\"]", a = i;
			switch (r) {
				case "audioworklet":
				case "paintworklet":
				case "serviceworker":
				case "sharedworker":
				case "worker":
				case "script": a = Rm(e);
			}
			if (!vm.has(a) && (e = T({
				rel: "modulepreload",
				href: e
			}, t), vm.set(a, e), n.querySelector(i) === null)) {
				switch (r) {
					case "audioworklet":
					case "paintworklet":
					case "serviceworker":
					case "sharedworker":
					case "worker":
					case "script": if (n.querySelector(zm(a))) return;
				}
				r = n.createElement("link"), np(r, "link", e), Wt(r), n.head.appendChild(r);
			}
		}
	}
	function Am(e, t, n) {
		xm.S(e, t, n);
		var r = wm;
		if (r && e) {
			var i = Ut(r).hoistableStyles, a = Pm(e);
			t ||= "default";
			var o = i.get(a);
			if (!o) {
				var s = {
					loading: 0,
					preload: null
				};
				if (o = r.querySelector(Fm(a))) s.loading = 5;
				else {
					e = T({
						rel: "stylesheet",
						href: e,
						"data-precedence": t
					}, n), (n = vm.get(a)) && Hm(e, n);
					var c = o = r.createElement("link");
					Wt(c), np(c, "link", e), c._p = new Promise(function(e, t) {
						c.onload = e, c.onerror = t;
					}), c.addEventListener("load", function() {
						s.loading |= 1;
					}), c.addEventListener("error", function() {
						s.loading |= 2;
					}), s.loading |= 4, Vm(o, t, r);
				}
				o = {
					type: "stylesheet",
					instance: o,
					count: 1,
					state: s
				}, i.set(a, o);
			}
		}
	}
	function jm(e, t) {
		xm.X(e, t);
		var n = wm;
		if (n && e) {
			var r = Ut(n).hoistableScripts, i = Rm(e), a = r.get(i);
			a || (a = n.querySelector(zm(i)), a || (e = T({
				src: e,
				async: !0
			}, t), (t = vm.get(i)) && Um(e, t), a = n.createElement("script"), Wt(a), np(a, "link", e), n.head.appendChild(a)), a = {
				type: "script",
				instance: a,
				count: 1,
				state: null
			}, r.set(i, a));
		}
	}
	function Mm(e, t) {
		xm.M(e, t);
		var n = wm;
		if (n && e) {
			var r = Ut(n).hoistableScripts, i = Rm(e), a = r.get(i);
			a || (a = n.querySelector(zm(i)), a || (e = T({
				src: e,
				async: !0,
				type: "module"
			}, t), (t = vm.get(i)) && Um(e, t), a = n.createElement("script"), Wt(a), np(a, "link", e), n.head.appendChild(a)), a = {
				type: "script",
				instance: a,
				count: 1,
				state: null
			}, r.set(i, a));
		}
	}
	function Nm(e, t, n, r) {
		var i = (i = Ae.current) ? bm(i) : null;
		if (!i) throw Error(s(446));
		switch (e) {
			case "meta":
			case "title": return null;
			case "style": return typeof n.precedence == "string" && typeof n.href == "string" ? (n = Pm(n.href), t = Ut(i).hoistableStyles, r = t.get(n), r || (r = {
				type: "style",
				instance: null,
				count: 0,
				state: null
			}, t.set(n, r)), r) : {
				type: "void",
				instance: null,
				count: 0,
				state: null
			};
			case "link":
				if (n.rel === "stylesheet" && typeof n.href == "string" && typeof n.precedence == "string") {
					e = Pm(n.href);
					var a = Ut(i).hoistableStyles, o = a.get(e);
					if (o || (i = i.ownerDocument || i, o = {
						type: "stylesheet",
						instance: null,
						count: 0,
						state: {
							loading: 0,
							preload: null
						}
					}, a.set(e, o), (a = i.querySelector(Fm(e))) ? a._p || (o.instance = a, o.state.loading = 5) : (a = vm.get(e), a || (a = {
						rel: "preload",
						as: "style",
						href: n.href,
						crossOrigin: n.crossOrigin,
						integrity: n.integrity,
						media: n.media,
						hrefLang: n.hrefLang,
						referrerPolicy: n.referrerPolicy
					}, vm.set(e, a)), Lm(i, e, a, o.state))), t && r === null) throw Error(s(528, ""));
					return o;
				}
				if (t && r !== null) throw Error(s(529, ""));
				return null;
			case "script": return t = n.async, n = n.src, typeof n == "string" && t && typeof t != "function" && typeof t != "symbol" ? (n = Rm(n), t = Ut(i).hoistableScripts, r = t.get(n), r || (r = {
				type: "script",
				instance: null,
				count: 0,
				state: null
			}, t.set(n, r)), r) : {
				type: "void",
				instance: null,
				count: 0,
				state: null
			};
			default: throw Error(s(444, e));
		}
	}
	function Pm(e) {
		return "href=\"" + un(e) + "\"";
	}
	function Fm(e) {
		return "link[rel=\"stylesheet\"][" + e + "]";
	}
	function Im(e) {
		return T({}, e, {
			"data-precedence": e.precedence,
			precedence: null
		});
	}
	function Lm(e, t, n, r) {
		if (t = e.querySelector("link[rel=\"preload\"][as=\"style\"][" + t + "]")) {
			if (!0 !== t[Rt]) {
				r.loading = 1;
				return;
			}
		} else t = e.createElement("link"), t[Rt] = !0, t.onload = t.onerror = Gt.bind(null, t), np(t, "link", n), Wt(t), e.head.appendChild(t);
		r.preload = t, t.addEventListener("load", function() {
			return r.loading |= 1;
		}), t.addEventListener("error", function() {
			return r.loading |= 2;
		});
	}
	function Rm(e) {
		return "[src=\"" + un(e) + "\"]";
	}
	function zm(e) {
		return "script[async]" + e;
	}
	function Bm(e, t, n) {
		if (t.count++, t.instance === null) switch (t.type) {
			case "style":
				var r = e.querySelector("style[data-href~=\"" + un(n.href) + "\"]");
				if (r) return t.instance = r, Wt(r), r;
				var i = T({}, n, {
					"data-href": n.href,
					"data-precedence": n.precedence,
					href: null,
					precedence: null
				});
				return r = (e.ownerDocument || e).createElement("style"), Wt(r), np(r, "style", i), Vm(r, n.precedence, e), t.instance = r;
			case "stylesheet":
				i = Pm(n.href);
				var a = e.querySelector(Fm(i));
				if (a) return t.state.loading |= 4, t.instance = a, Wt(a), a;
				r = Im(n), (i = vm.get(i)) && Hm(r, i), a = (e.ownerDocument || e).createElement("link"), Wt(a);
				var o = a;
				return o._p = new Promise(function(e, t) {
					o.onload = e, o.onerror = t;
				}), np(a, "link", r), t.state.loading |= 4, Vm(a, n.precedence, e), t.instance = a;
			case "script": return a = Rm(n.src), (i = e.querySelector(zm(a))) ? (t.instance = i, Wt(i), i) : (r = n, (i = vm.get(a)) && (r = T({}, n), Um(r, i)), e = e.ownerDocument || e, i = e.createElement("script"), Wt(i), np(i, "link", r), e.head.appendChild(i), t.instance = i);
			case "void": return null;
			default: throw Error(s(443, t.type));
		}
		else t.type === "stylesheet" && !(t.state.loading & 4) && (r = t.instance, t.state.loading |= 4, Vm(r, n.precedence, e));
		return t.instance;
	}
	function Vm(e, t, n) {
		for (var r = n.querySelectorAll("link[rel=\"stylesheet\"][data-precedence],style[data-precedence]"), i = r.length ? r[r.length - 1] : null, a = i, o = 0; o < r.length; o++) {
			var s = r[o];
			if (s.dataset.precedence === t) a = s;
			else if (a !== i) break;
		}
		a ? a.parentNode.insertBefore(e, a.nextSibling) : (t = n.nodeType === 9 ? n.head : n, t.insertBefore(e, t.firstChild));
	}
	function Hm(e, t) {
		e.crossOrigin ??= t.crossOrigin, e.referrerPolicy ??= t.referrerPolicy, e.title ??= t.title;
	}
	function Um(e, t) {
		e.crossOrigin ??= t.crossOrigin, e.referrerPolicy ??= t.referrerPolicy, e.integrity ??= t.integrity;
	}
	var Wm = null;
	function Gm(e, t, n) {
		if (Wm === null) {
			var r = /* @__PURE__ */ new Map(), i = Wm = /* @__PURE__ */ new Map();
			i.set(n, r);
		} else i = Wm, r = i.get(n), r || (r = /* @__PURE__ */ new Map(), i.set(n, r));
		if (r.has(e)) return r;
		for (r.set(e, null), n = n.getElementsByTagName(e), i = 0; i < n.length; i++) {
			var a = n[i];
			if (!(a[Lt] || a[At] || e === "link" && a.getAttribute("rel") === "stylesheet") && a.namespaceURI !== "http://www.w3.org/2000/svg") {
				var o = a.getAttribute(t) || "";
				o = e + o;
				var s = r.get(o);
				s ? s.push(a) : r.set(o, [a]);
			}
		}
		return r;
	}
	function Km(e, t, n) {
		e = e.ownerDocument || e, e.head.insertBefore(n, t === "title" ? e.querySelector("head > title") : null);
	}
	function qm(e, t, n) {
		if (n === 1 || t.itemProp != null) return !1;
		switch (e) {
			case "meta":
			case "title": return !0;
			case "style":
				if (typeof t.precedence != "string" || typeof t.href != "string" || t.href === "") break;
				return !0;
			case "link":
				if (typeof t.rel != "string" || typeof t.href != "string" || t.href === "" || t.onLoad || t.onError) break;
				switch (t.rel) {
					case "stylesheet": return e = t.disabled, typeof t.precedence == "string" && e == null;
					default: return !0;
				}
			case "script": if (t.async && typeof t.async != "function" && typeof t.async != "symbol" && !t.onLoad && !t.onError && t.src && typeof t.src == "string") return !0;
		}
		return !1;
	}
	function Jm(e, t) {
		return e === "img" && t.src != null && t.src !== "" && t.onLoad == null && t.loading !== "lazy";
	}
	function Ym(e) {
		return !(e.type === "stylesheet" && !(e.state.loading & 3));
	}
	function Xm(e) {
		return (e.width || 100) * (e.height || 100) * (typeof devicePixelRatio == "number" ? devicePixelRatio : 1) * .25;
	}
	function Zm(e, t) {
		typeof t.decode == "function" && (e.imgCount++, t.complete || (e.imgBytes += Xm(t), e.suspenseyImages.push(t)), e = rh.bind(e), t.decode().then(e, e));
	}
	function Qm(e, t, n, r) {
		if (n.type === "stylesheet" && (typeof r.media != "string" || !1 !== matchMedia(r.media).matches) && !(n.state.loading & 4)) {
			if (n.instance === null) {
				var i = Pm(r.href), a = t.querySelector(Fm(i));
				if (a) {
					t = a._p, typeof t == "object" && t && typeof t.then == "function" && (e.count++, e = nh.bind(e), t.then(e, e)), n.state.loading |= 4, n.instance = a, Wt(a);
					return;
				}
				a = t.ownerDocument || t, r = Im(r), (i = vm.get(i)) && Hm(r, i), a = a.createElement("link"), Wt(a);
				var o = a;
				o._p = new Promise(function(e, t) {
					o.onload = e, o.onerror = t;
				}), np(a, "link", r), n.instance = a;
			}
			e.stylesheets === null && (e.stylesheets = /* @__PURE__ */ new Map()), e.stylesheets.set(n, t), (t = n.state.preload) && !(n.state.loading & 3) && (e.count++, n = nh.bind(e), t.addEventListener("load", n), t.addEventListener("error", n));
		}
	}
	var $m = 0;
	function eh(e, t) {
		return e.stylesheets && e.count === 0 && ah(e, e.stylesheets), 0 < e.count || 0 < e.imgCount ? function(n) {
			var r = setTimeout(function() {
				if (e.stylesheets && ah(e, e.stylesheets), e.unsuspend) {
					var t = e.unsuspend;
					e.unsuspend = null, t();
				}
			}, 6e4 + t);
			0 < e.imgBytes && $m === 0 && ($m = 62500 * op());
			var i = setTimeout(function() {
				if (e.waitingForImages = !1, e.count === 0 && (e.stylesheets && ah(e, e.stylesheets), e.unsuspend)) {
					var t = e.unsuspend;
					e.unsuspend = null, t();
				}
			}, (e.imgBytes > $m ? 50 : 800) + t);
			return e.unsuspend = n, function() {
				e.unsuspend = null, clearTimeout(r), clearTimeout(i);
			};
		} : null;
	}
	function th(e) {
		if (e.count === 0 && (e.imgCount === 0 || !e.waitingForImages)) {
			if (e.stylesheets) ah(e, e.stylesheets);
			else if (e.unsuspend) {
				var t = e.unsuspend;
				e.unsuspend = null, t();
			}
		}
	}
	function nh() {
		this.count--, th(this);
	}
	function rh() {
		this.imgCount--, th(this);
	}
	var ih = null;
	function ah(e, t) {
		e.stylesheets = null, e.unsuspend !== null && (e.count++, ih = /* @__PURE__ */ new Map(), t.forEach(oh, e), ih = null, nh.call(e));
	}
	function oh(e, t) {
		if (!(t.state.loading & 4)) {
			var n = ih.get(e);
			if (n) var r = n.get(null);
			else {
				n = /* @__PURE__ */ new Map(), ih.set(e, n);
				for (var i = e.querySelectorAll("link[data-precedence],style[data-precedence]"), a = 0; a < i.length; a++) {
					var o = i[a];
					(o.nodeName === "LINK" || o.getAttribute("media") !== "not all") && (n.set(o.dataset.precedence, o), r = o);
				}
				r && n.set(null, r);
			}
			i = t.instance, o = i.getAttribute("data-precedence"), a = n.get(o) || r, a === r && n.set(null, i), n.set(o, i), this.count++, r = nh.bind(this), i.addEventListener("load", r), i.addEventListener("error", r), a ? a.parentNode.insertBefore(i, a.nextSibling) : (e = e.nodeType === 9 ? e.head : e, e.insertBefore(i, e.firstChild)), t.state.loading |= 4;
		}
	}
	var sh = {
		$$typeof: se,
		Provider: null,
		Consumer: null,
		_currentValue: Ce,
		_currentValue2: Ce,
		_threadCount: 0
	};
	function ch(e, t, n, r, i, a, o, s, c) {
		this.tag = 1, this.containerInfo = e, this.pingCache = this.current = this.pendingChildren = null, this.timeoutHandle = -1, this.callbackNode = this.next = this.pendingContext = this.context = this.cancelPendingCommit = null, this.callbackPriority = 0, this.expirationTimes = yt(-1), this.entangledLanes = this.shellSuspendCounter = this.errorRecoveryDisabledLanes = this.expiredLanes = this.warmLanes = this.pingedLanes = this.suspendedLanes = this.pendingLanes = 0, this.entanglements = yt(0), this.hiddenUpdates = yt(null), this.identifierPrefix = r, this.onUncaughtError = i, this.onCaughtError = a, this.onRecoverableError = o, this.pooledCache = null, this.pooledCacheLanes = 0, this.formState = c, this.transitionTypes = null, this.incompleteTransitions = /* @__PURE__ */ new Map();
	}
	function lh(e, t, n, r, i, a, o, s, c, l, u, d) {
		return e = new ch(e, t, n, o, c, l, u, d, s), t = 1, !0 === a && (t |= 24), a = Fi(3, null, null, t), e.current = a, a.stateNode = e, t = Pa(), t.refCount++, e.pooledCache = t, t.refCount++, a.memoizedState = {
			element: r,
			isDehydrated: n,
			cache: t
		}, _o(a), e;
	}
	function uh(e) {
		return e ? (e = Ni, e) : Ni;
	}
	function dh(e, t, n, r, i, a) {
		i = uh(i), r.context === null ? r.context = i : r.pendingContext = i, r = yo(t), r.payload = { element: n }, a = a === void 0 ? null : a, a !== null && (r.callback = a), n = bo(e, r, t), n !== null && (Pd(n, e, t), xo(n, e, t));
	}
	function fh(e, t) {
		if (e = e.memoizedState, e !== null && e.dehydrated !== null) {
			var n = e.retryLane;
			e.retryLane = n !== 0 && n < t ? n : t;
		}
	}
	function ph(e, t) {
		fh(e, t), (e = e.alternate) && fh(e, t);
	}
	function mh(e) {
		if (e.tag === 13 || e.tag === 31) {
			var t = Ai(e, 67108864);
			t !== null && Pd(t, e, 67108864), ph(e, 67108864);
		}
	}
	function hh(e) {
		if (e.tag === 13 || e.tag === 31) {
			var t = jd();
			t = Tt(t);
			var n = Ai(e, t);
			n !== null && Pd(n, e, t), ph(e, t);
		}
	}
	var gh = !0;
	function _h(e, t, n, r) {
		var i = O.T;
		O.T = null;
		var a = k.p;
		try {
			k.p = 2, yh(e, t, n, r);
		} finally {
			k.p = a, O.T = i;
		}
	}
	function vh(e, t, n, r) {
		var i = O.T;
		O.T = null;
		var a = k.p;
		try {
			k.p = 8, yh(e, t, n, r);
		} finally {
			k.p = a, O.T = i;
		}
	}
	function yh(e, t, n, r) {
		if (gh) {
			var i = bh(r);
			if (i === null) Kf(e, t, r, xh, n), Mh(e, r);
			else if (Ph(i, e, t, n, r)) r.stopPropagation();
			else if (Mh(e, r), t & 4 && -1 < jh.indexOf(e)) {
				for (; i !== null;) {
					var a = Vt(i);
					if (a !== null) switch (a.tag) {
						case 3:
							if (a = a.stateNode, a.current.memoizedState.isDehydrated) {
								var o = pt(a.pendingLanes);
								if (o !== 0) {
									var s = a;
									for (s.pendingLanes |= 2, s.entangledLanes |= 2; o;) {
										var c = 1 << 31 - ot(o);
										s.entanglements[1] |= c, o &= ~c;
									}
									Ef(a), !(G & 6) && (gd = Je() + 500, Df(0, !1));
								}
							}
							break;
						case 31:
						case 13: s = Ai(a, 2), s !== null && Pd(s, a, 2), zd(), ph(a, 2);
					}
					if (a = bh(r), a === null && Kf(e, t, r, xh, n), a === i) break;
					i = a;
				}
				i !== null && r.stopPropagation();
			} else Kf(e, t, r, null, n);
		}
	}
	function bh(e) {
		return e = Dn(e), Sh(e);
	}
	var xh = null;
	function Sh(e) {
		if (xh = null, e = Bt(e), e !== null) {
			var t = l(e);
			if (t === null) e = null;
			else {
				var n = t.tag;
				if (n === 13) {
					if (e = u(t), e !== null) return e;
					e = null;
				} else if (n === 31) {
					if (e = d(t), e !== null) return e;
					e = null;
				} else if (n === 3) {
					if (t.stateNode.current.memoizedState.isDehydrated) return t.tag === 3 ? t.stateNode.containerInfo : null;
					e = null;
				} else t !== e && (e = null);
			}
		}
		return xh = e, null;
	}
	function Ch(e) {
		switch (e) {
			case "beforetoggle":
			case "cancel":
			case "click":
			case "close":
			case "contextmenu":
			case "copy":
			case "cut":
			case "auxclick":
			case "dblclick":
			case "dragend":
			case "dragstart":
			case "drop":
			case "focusin":
			case "focusout":
			case "input":
			case "invalid":
			case "keydown":
			case "keypress":
			case "keyup":
			case "mousedown":
			case "mouseup":
			case "paste":
			case "pause":
			case "play":
			case "pointercancel":
			case "pointerdown":
			case "pointerup":
			case "ratechange":
			case "reset":
			case "seeked":
			case "submit":
			case "toggle":
			case "touchcancel":
			case "touchend":
			case "touchstart":
			case "volumechange":
			case "change":
			case "selectionchange":
			case "textInput":
			case "compositionstart":
			case "compositionend":
			case "compositionupdate":
			case "beforeblur":
			case "afterblur":
			case "beforeinput":
			case "blur":
			case "fullscreenchange":
			case "fullscreenerror":
			case "focus":
			case "hashchange":
			case "popstate":
			case "select":
			case "selectstart": return 2;
			case "drag":
			case "dragenter":
			case "dragexit":
			case "dragleave":
			case "dragover":
			case "mousemove":
			case "mouseout":
			case "mouseover":
			case "pointermove":
			case "pointerout":
			case "pointerover":
			case "resize":
			case "scroll":
			case "touchmove":
			case "wheel":
			case "mouseenter":
			case "mouseleave":
			case "pointerenter":
			case "pointerleave": return 8;
			case "message": switch (Ye()) {
				case Xe: return 2;
				case Ze: return 8;
				case Qe:
				case $e: return 32;
				case et: return 268435456;
				default: return 32;
			}
			default: return 32;
		}
	}
	var wh = !1, Th = null, Eh = null, Dh = null, Oh = /* @__PURE__ */ new Map(), kh = /* @__PURE__ */ new Map(), Ah = [], jh = "mousedown mouseup touchcancel touchend touchstart auxclick dblclick pointercancel pointerdown pointerup dragend dragstart drop compositionend compositionstart keydown keypress keyup input textInput copy cut paste click change contextmenu reset".split(" ");
	function Mh(e, t) {
		switch (e) {
			case "focusin":
			case "focusout":
				Th = null;
				break;
			case "dragenter":
			case "dragleave":
				Eh = null;
				break;
			case "mouseover":
			case "mouseout":
				Dh = null;
				break;
			case "pointerover":
			case "pointerout":
				Oh.delete(t.pointerId);
				break;
			case "gotpointercapture":
			case "lostpointercapture": kh.delete(t.pointerId);
		}
	}
	function Nh(e, t, n, r, i, a) {
		return e === null || e.nativeEvent !== a ? (e = {
			blockedOn: t,
			domEventName: n,
			eventSystemFlags: r,
			nativeEvent: a,
			targetContainers: [i]
		}, t !== null && (t = Vt(t), t !== null && mh(t)), e) : (e.eventSystemFlags |= r, t = e.targetContainers, i !== null && t.indexOf(i) === -1 && t.push(i), e);
	}
	function Ph(e, t, n, r, i) {
		switch (t) {
			case "focusin": return Th = Nh(Th, e, t, n, r, i), !0;
			case "dragenter": return Eh = Nh(Eh, e, t, n, r, i), !0;
			case "mouseover": return Dh = Nh(Dh, e, t, n, r, i), !0;
			case "pointerover":
				var a = i.pointerId;
				return Oh.set(a, Nh(Oh.get(a) || null, e, t, n, r, i)), !0;
			case "gotpointercapture": return a = i.pointerId, kh.set(a, Nh(kh.get(a) || null, e, t, n, r, i)), !0;
		}
		return !1;
	}
	function Fh(e) {
		var t = Bt(e.target);
		if (t !== null) {
			var n = l(t);
			if (n !== null) {
				if (t = n.tag, t === 13) {
					if (t = u(n), t !== null) {
						e.blockedOn = t, Ot(e.priority, function() {
							hh(n);
						});
						return;
					}
				} else if (t === 31) {
					if (t = d(n), t !== null) {
						e.blockedOn = t, Ot(e.priority, function() {
							hh(n);
						});
						return;
					}
				} else if (t === 3 && n.stateNode.current.memoizedState.isDehydrated) {
					e.blockedOn = n.tag === 3 ? n.stateNode.containerInfo : null;
					return;
				}
			}
		}
		e.blockedOn = null;
	}
	function Ih(e) {
		if (e.blockedOn !== null) return !1;
		for (var t = e.targetContainers; 0 < t.length;) {
			var n = bh(e.nativeEvent);
			if (n === null) {
				n = e.nativeEvent;
				var r = new n.constructor(n.type, n);
				En = r, n.target.dispatchEvent(r), En = null;
			} else return t = Vt(n), t !== null && mh(t), e.blockedOn = n, !1;
			t.shift();
		}
		return !0;
	}
	function Lh(e, t, n) {
		Ih(e) && n.delete(t);
	}
	function Rh() {
		wh = !1, Th !== null && Ih(Th) && (Th = null), Eh !== null && Ih(Eh) && (Eh = null), Dh !== null && Ih(Dh) && (Dh = null), Oh.forEach(Lh), kh.forEach(Lh);
	}
	function zh(e, n) {
		e.blockedOn === n && (e.blockedOn = null, wh || (wh = !0, t.unstable_scheduleCallback(t.unstable_NormalPriority, Rh)));
	}
	var Bh = null;
	function Vh(e) {
		Bh !== e && (Bh = e, t.unstable_scheduleCallback(t.unstable_NormalPriority, function() {
			Bh === e && (Bh = null);
			for (var t = 0; t < e.length; t += 3) {
				var n = e[t], r = e[t + 1], i = e[t + 2];
				if (typeof r != "function") {
					if (Sh(r || n) === null) continue;
					break;
				}
				var a = Vt(n);
				a !== null && (e.splice(t, 3), t -= 3, nc(a, {
					pending: !0,
					data: i,
					method: n.method,
					action: r
				}, r, i));
			}
		}));
	}
	function Hh(e) {
		function t(t) {
			return zh(t, e);
		}
		Th !== null && zh(Th, e), Eh !== null && zh(Eh, e), Dh !== null && zh(Dh, e), Oh.forEach(t), kh.forEach(t);
		for (var n = 0; n < Ah.length; n++) {
			var r = Ah[n];
			r.blockedOn === e && (r.blockedOn = null);
		}
		for (; 0 < Ah.length && (n = Ah[0], n.blockedOn === null);) Fh(n), n.blockedOn === null && Ah.shift();
		if (n = (e.ownerDocument || e).$$reactFormReplay, n != null) for (r = 0; r < n.length; r += 3) {
			var i = n[r], a = n[r + 1], o = i[jt] || null;
			if (typeof a == "function") o || Vh(n);
			else if (o) {
				var s = null;
				if (a && a.hasAttribute("formAction")) {
					if (i = a, o = a[jt] || null) s = o.formAction;
					else if (Sh(i) !== null) continue;
				} else s = o.action;
				typeof s == "function" ? n[r + 1] = s : (n.splice(r, 3), r -= 3), Vh(n);
			}
		}
	}
	function Uh() {
		function e(e) {
			e.canIntercept && e.info === "react-transition" && e.intercept({
				handler: function() {
					return new Promise(function(e) {
						return i = e;
					});
				},
				focusReset: "manual",
				scroll: "manual"
			});
		}
		function t() {
			i !== null && (i(), i = null), r || setTimeout(n, 20);
		}
		function n() {
			if (!r && !navigation.transition) {
				var e = navigation.currentEntry;
				e && e.url != null && navigation.navigate(e.url, {
					state: e.getState(),
					info: "react-transition",
					history: "replace"
				});
			}
		}
		if (typeof navigation == "object") {
			var r = !1, i = null;
			return navigation.addEventListener("navigate", e), navigation.addEventListener("navigatesuccess", t), navigation.addEventListener("navigateerror", t), setTimeout(n, 100), function() {
				r = !0, navigation.removeEventListener("navigate", e), navigation.removeEventListener("navigatesuccess", t), navigation.removeEventListener("navigateerror", t), i !== null && (i(), i = null);
			};
		}
	}
	function Wh(e) {
		this._internalRoot = e;
	}
	Gh.prototype.render = Wh.prototype.render = function(e) {
		var t = this._internalRoot;
		if (t === null) throw Error(s(409));
		var n = t.current;
		dh(n, jd(), e, t, null, null);
	}, Gh.prototype.unmount = Wh.prototype.unmount = function() {
		var e = this._internalRoot;
		if (e !== null) {
			this._internalRoot = null;
			var t = e.containerInfo;
			dh(e.current, 2, null, e, null, null), zd(), t[Mt] = null;
		}
	};
	function Gh(e) {
		this._internalRoot = e;
	}
	Gh.prototype.unstable_scheduleHydration = function(e) {
		if (e) {
			var t = Dt();
			e = {
				blockedOn: null,
				target: e,
				priority: t
			};
			for (var n = 0; n < Ah.length && t !== 0 && t < Ah[n].priority; n++);
			Ah.splice(n, 0, e), n === 0 && Fh(e);
		}
	};
	var Kh = r.version;
	if (Kh !== "19.3.0") throw Error(s(527, Kh, "19.3.0"));
	k.findDOMNode = function(e) {
		var t = e._reactInternals;
		if (t === void 0) throw typeof e.render == "function" ? Error(s(188)) : (e = Object.keys(e).join(","), Error(s(268, e)));
		return e = p(t), e = e === null ? null : m(e), e = e === null ? null : e.stateNode, e;
	};
	var qh = {
		bundleType: 0,
		version: "19.3.0",
		rendererPackageName: "react-dom",
		currentDispatcherRef: O,
		reconcilerVersion: "19.3.0"
	};
	if (typeof __REACT_DEVTOOLS_GLOBAL_HOOK__ < "u") {
		var Jh = __REACT_DEVTOOLS_GLOBAL_HOOK__;
		if (!Jh.isDisabled && Jh.supportsFiber) try {
			rt = Jh.inject(qh), it = Jh;
		} catch {}
	}
	e.createRoot = function(e, t) {
		if (!c(e)) throw Error(s(299));
		var n = !1, r = "", i = Tc, a = Ec, o = Dc;
		return t != null && (!0 === t.unstable_strictMode && (n = !0), t.identifierPrefix !== void 0 && (r = t.identifierPrefix), t.onUncaughtError !== void 0 && (i = t.onUncaughtError), t.onCaughtError !== void 0 && (a = t.onCaughtError), t.onRecoverableError !== void 0 && (o = t.onRecoverableError)), t = lh(e, 1, !1, null, null, n, r, null, i, a, o, Uh), e[Mt] = t.current, Wf(e), new Wh(t);
	};
})), c = /* @__PURE__ */ e(((e, t) => {
	function n() {
		if (!(typeof __REACT_DEVTOOLS_GLOBAL_HOOK__ > "u" || typeof __REACT_DEVTOOLS_GLOBAL_HOOK__.checkDCE != "function")) try {
			__REACT_DEVTOOLS_GLOBAL_HOOK__.checkDCE(n);
		} catch (e) {
			console.error(e);
		}
	}
	n(), t.exports = s();
})), l = n(), u = o(), d = c(), f = 2e3, p = 5e3, m = {
	inactive: "No live worker",
	opening: "Opening on host",
	idle: "Host worker idle",
	running: "Running on host",
	permission: "Running on host · approval needed",
	input: "Running on host · question needs an answer",
	switching: "Host worker switching conversation",
	closing: "Host worker closing",
	failed: "Host worker failed · review required",
	unknown: "Host state unavailable"
}, h = {
	registered: "Registered projects",
	running: "Host-running projects",
	permissions: "Approvals needed",
	questions: "Question requests",
	failed: "Failed projects",
	recovery: "Recovery reviews",
	queued: "Queued follow-ups",
	review: "Queue items to review"
}, g = [
	"",
	"bound",
	"admission_unknown",
	"admitted",
	"completed",
	"failed",
	"canceled",
	"rejected"
], _ = [
	"available",
	"missing",
	"changed",
	"unavailable"
];
function v(e) {
	return typeof e == "object" && !!e && !Array.isArray(e);
}
function y(e) {
	return typeof e == "string" && /^[A-Za-z0-9_-]{1,128}$/.test(e);
}
function b(e, t) {
	return typeof e == "number" && Number.isSafeInteger(e) && e >= 0 && e <= t;
}
function x(e, t = "") {
	if (!y(e) || t !== "" && !y(t)) throw Error("identifier");
	let n = new URLSearchParams({
		view: "projects",
		project: e
	});
	return t && n.set("session", t), "/?" + n.toString();
}
function S(e, t, n = "") {
	if (typeof e != "string" || e.length > 512 || !e.startsWith("/?") || /[\s\\#]/.test(e)) return !1;
	let r = new URL(e, "https://activity.invalid"), i = r.searchParams;
	return r.origin === "https://activity.invalid" && r.pathname === "/" && i.size === (n ? 3 : 2) && i.getAll("view").length === 1 && i.get("view") === "projects" && i.getAll("project").length === 1 && i.get("project") === t && (n ? i.getAll("session").length === 1 && i.get("session") === n : !i.has("session"));
}
function ee(e) {
	if (!v(e) || !Array.isArray(e.projects) || e.projects.length > 100 || typeof e.updated_at != "string" || e.updated_at.length > 64 || !Number.isFinite(Date.parse(e.updated_at))) throw Error("summary");
	if (!v(e.counts)) throw Error("counts");
	for (let t of Object.keys(h)) if (!b(e.counts[t], t === "queued" || t === "review" ? 800 : 100)) throw Error("counts");
	let t = /* @__PURE__ */ new Set();
	for (let n of e.projects) {
		if (!v(n) || !y(n.project_id) || t.has(n.project_id) || typeof n.name != "string" || n.name.length > 128 || typeof n.runtime_state != "string" || !Object.hasOwn(m, n.runtime_state) || !g.some((e) => e === n.recovery_state) || !_.some((e) => e === n.folder_state)) throw Error("project");
		if (n.session_id !== "" && !y(n.session_id)) throw Error("session");
		if (!S(n.project_url, n.project_id) || (n.session_id === "" ? n.session_url !== "" : !S(n.session_url, n.project_id, n.session_id))) throw Error("navigation");
		if ([
			"host_running",
			"failed",
			"recovery",
			"unavailable"
		].some((e) => typeof n[e] != "boolean") || !b(n.permissions, 1) || !b(n.questions, 1) || !b(n.queued, 8) || !b(n.review, 8) || n.queued + n.review > 8) throw Error("state");
		t.add(n.project_id);
	}
	return e;
}
async function C(e, t) {
	t.throwIfAborted();
	let n = e.headers.get("Content-Length");
	if (n !== null && (!/^\d+$/.test(n) || Number(n) > 262144)) throw Error("size");
	if (e.headers.get("Content-Type")?.split(";", 1)[0]?.trim().toLowerCase() !== "application/json") throw Error("type");
	if (!e.body) throw Error("body");
	let r = e.body.getReader(), i = [], a = 0, o = () => {
		r.cancel().catch(() => {});
	};
	t.addEventListener("abort", o, { once: !0 });
	try {
		for (;;) {
			t.throwIfAborted();
			let { done: e, value: n } = await r.read();
			if (t.throwIfAborted(), e) break;
			if (a += n.byteLength, a > 262144) throw Error("size");
			i.push(n);
		}
		let e = new Uint8Array(a), n = 0;
		for (let t of i) e.set(t, n), n += t.byteLength;
		return t.throwIfAborted(), ee(JSON.parse(new TextDecoder("utf-8", { fatal: !0 }).decode(e)));
	} finally {
		t.removeEventListener("abort", o), r.cancel().catch(() => {}), r.releaseLock();
	}
}
var w = class extends Error {};
async function te(e, t = fetch) {
	e.throwIfAborted();
	let n = await t("/activity", {
		method: "GET",
		credentials: "same-origin",
		cache: "no-store",
		redirect: "error",
		headers: { Accept: "application/json" },
		signal: e
	});
	try {
		if (e.throwIfAborted(), n.status === 401 || n.status === 403) throw new w();
		if (!n.ok) throw Error("refresh");
		return await C(n, e);
	} catch (e) {
		throw n.body?.cancel().catch(() => {}), e;
	}
}
//#endregion
//#region src/activity/useActivity.ts
var T = "Connecting this browser to the activity summary…", ne = "Summary paused. Displayed host state may be stale.", E = {
	summary: null,
	freshness: "loading",
	message: T,
	busy: !1,
	accessEnded: !1
};
function re(e, t) {
	let [n, r] = (0, l.useState)(E), i = (0, l.useRef)(() => {});
	return (0, l.useEffect)(() => {
		let n = !1, a = !1, o = !1, s = !1, c = null, l = null, u, d, m = () => t && !s && !c && document.visibilityState !== "hidden" && !!e.current?.isConnected && e.current.closest("#workspace")?.dataset.view === "activity" && e.current.getClientRects().length > 0, h = () => !n && !o && a && m(), g = () => {
			clearTimeout(u), clearTimeout(d);
			let e = l;
			l = null, e?.abort();
		}, _ = () => {
			o = !0, a = !1, g(), r({
				summary: null,
				freshness: "error",
				busy: !1,
				accessEnded: !0,
				message: "Browser access ended. Pair this browser again; host work was not stopped."
			});
		}, v = async () => {
			if (!h() || l) return;
			clearTimeout(u);
			let e = new AbortController();
			l = e, r((e) => ({
				...e,
				busy: !0
			})), d = setTimeout(() => e.abort(), p);
			try {
				let t = await te(e.signal);
				if (!h() || l !== e || e.signal.aborted) return;
				r({
					summary: t,
					freshness: "fresh",
					busy: !1,
					accessEnded: !1,
					message: "Browser connected · summary refreshed. Host work is independent of this connection."
				});
			} catch (t) {
				if (!h() || l !== e) return;
				if (t instanceof w) {
					_();
					return;
				}
				r((e) => ({
					...e,
					freshness: "error",
					busy: !1,
					message: "Browser could not refresh activity. Displayed host state may be stale; no work was replayed."
				}));
			} finally {
				!n && l === e && (clearTimeout(d), l = null, h() && (u = setTimeout(() => {
					v();
				}, f)));
			}
		}, y = () => {
			if (!(n || o)) {
				if (!m()) {
					a && (a = !1, g(), r((e) => ({
						...e,
						freshness: "paused",
						message: ne,
						busy: !1
					})));
					return;
				}
				a || (a = !0, r({ ...E }), v());
			}
		};
		i.current = () => {
			h() && !l && v();
		};
		let b = () => {
			s = !0, y();
		}, x = () => {
			s = !1, y();
		}, S = (e) => {
			if (!(e.target instanceof HTMLFormElement)) return;
			let t = new URL(e.target.action, window.location.href);
			t.origin === window.location.origin && ["/logout", "/access/revoke-all"].includes(t.pathname) && _();
		}, ee = (e) => e.detail || {}, C = (t) => {
			let n = ee(t);
			if (["/logout", "/access/revoke-all"].includes(n.requestConfig?.path || "")) {
				_();
				return;
			}
			n.target instanceof Element && e.current && n.target.contains(e.current) && (c = n.target, y());
		}, T = (e) => {
			let t = ee(e).target;
			!c || t instanceof Element && t !== c || (c = null, y());
		};
		document.addEventListener("snow:navigation-start", C), document.addEventListener("snow:navigation-before-swap", C), document.addEventListener("snow:navigation-end", T), document.addEventListener("snow:navigation-after-swap", T), document.addEventListener("visibilitychange", y), document.addEventListener("submit", S, !0), window.addEventListener("pagehide", b), window.addEventListener("pageshow", x);
		let re = new MutationObserver(y);
		return re.observe(document.documentElement, {
			childList: !0,
			subtree: !0,
			attributes: !0,
			attributeFilter: ["data-view", "hidden"]
		}), r(t ? { ...E } : {
			...E,
			freshness: "error",
			message: "The project registry is unavailable. Activity cannot be loaded."
		}), y(), () => {
			n = !0, a = !1, g(), i.current = () => {}, re.disconnect(), document.removeEventListener("snow:navigation-start", C), document.removeEventListener("snow:navigation-before-swap", C), document.removeEventListener("snow:navigation-end", T), document.removeEventListener("snow:navigation-after-swap", T), document.removeEventListener("visibilitychange", y), document.removeEventListener("submit", S, !0), window.removeEventListener("pagehide", b), window.removeEventListener("pageshow", x);
		};
	}, [e, t]), {
		...n,
		refresh: () => i.current()
	};
}
//#endregion
//#region node_modules/react/cjs/react-jsx-runtime.production.js
var ie = /* @__PURE__ */ e(((e) => {
	var t = Symbol.for("react.transitional.element"), n = Symbol.for("react.fragment");
	function r(e, n, r) {
		var i = null;
		if (r !== void 0 && (i = "" + r), n.key !== void 0 && (i = "" + n.key), "key" in n) for (var a in r = {}, n) a !== "key" && (r[a] = n[a]);
		else r = n;
		return n = r.ref, {
			$$typeof: t,
			type: e,
			key: i,
			ref: n === void 0 ? null : n,
			props: r
		};
	}
	e.Fragment = n, e.jsx = r, e.jsxs = r;
})), D = (/* @__PURE__ */ e(((e, t) => {
	t.exports = ie();
})))();
function ae({ counts: e }) {
	return Object.keys(h).map((t) => /* @__PURE__ */ (0, D.jsxs)("dl", {
		className: "manager-activity-count",
		children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: h[t] }), /* @__PURE__ */ (0, D.jsx)("dd", { children: e[t] })]
	}, t));
}
function oe({ project: e }) {
	let t = [];
	e.permissions && t.push("Approval needed. Review it in the conversation."), e.questions && t.push("A question request needs your answer in the conversation."), e.failed && t.push("Failure observed. Review before explicitly retrying."), e.recovery && t.push("Recovery review needed; completion or admission may be uncertain. Nothing is replayed."), e.queued && t.push(`${e.queued} queued follow-up${e.queued === 1 ? "" : "s"}.`), e.review && t.push(`${e.review} retained queue item${e.review === 1 ? "" : "s"} to review. No automatic resend.`), e.folder_state !== "available" && t.push("Host folder is unavailable or changed. Review the registration."), e.unavailable && t.push("Some summary metadata is unavailable. Open the project to review."), t.length || t.push(e.session_id ? "Open the saved conversation to review its current state." : "No attention requested. Opening this project does not activate an agent.");
	let n = !!(e.permissions || e.questions || e.failed || e.recovery || e.review || e.unavailable);
	return /* @__PURE__ */ (0, D.jsxs)("article", {
		className: "manager-activity-card",
		"data-project": e.project_id,
		"data-attention": String(n),
		children: [
			/* @__PURE__ */ (0, D.jsx)("h3", { children: e.name || "Registered project" }),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "manager-activity-state",
				children: m[e.runtime_state]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "manager-activity-details",
				children: t.join(" ")
			}),
			/* @__PURE__ */ (0, D.jsx)("a", {
				className: "button manager-activity-link",
				href: x(e.project_id, e.session_id),
				"data-snow-navigation": "",
				"aria-label": `${e.session_id ? "Open conversation" : "Open project"} in ${e.name || "registered project"}`,
				children: e.session_id ? "Open this conversation →" : "Open project →"
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "manager-activity-session mono",
				children: e.session_id ? "Session " + e.session_id : ""
			})
		]
	});
}
function se({ registryEnabled: e, error: t }) {
	let n = (0, l.useRef)(null), r = re(n, e);
	return /* @__PURE__ */ (0, D.jsxs)("section", {
		ref: n,
		className: "manager-activity",
		"data-manager-activity": "",
		"data-freshness": r.freshness,
		"aria-labelledby": "manager-activity-title",
		children: [
			/* @__PURE__ */ (0, D.jsxs)("header", {
				className: "workspace-heading",
				children: [/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("span", {
					className: "eyebrow",
					children: "ACROSS YOUR PROJECTS"
				}), /* @__PURE__ */ (0, D.jsx)("h1", {
					id: "manager-activity-title",
					children: "Activity & attention"
				})] }), /* @__PURE__ */ (0, D.jsx)("span", {
					className: "pill",
					children: "Read-only"
				})]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "manager-activity-intro",
				children: "See what is running on this host and what needs your attention. Open the exact conversation to review an approval, answer a question, stop work, or inspect retained queue items."
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "manager-activity-connection",
				children: [/* @__PURE__ */ (0, D.jsx)("p", {
					"data-manager-activity-fresh": "",
					role: "status",
					children: r.message
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-manager-activity-refresh": "",
					onClick: r.refresh,
					disabled: !e || r.busy || r.accessEnded || r.freshness === "paused",
					children: "Refresh summary"
				})]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				"data-manager-activity-time": "",
				children: r.summary ? "Host snapshot: " + new Date(r.summary.updated_at).toLocaleTimeString() + ". Counts can overlap; questions count pending requests, not individual prompts." : ""
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "manager-activity-note fine",
				children: "Host-running is not browser-connected. Leaving this page or losing the browser connection does not stop admitted work. Refreshing only reads current state; it never activates workers or replays work."
			}),
			t && /* @__PURE__ */ (0, D.jsx)("p", {
				className: "error",
				role: "alert",
				children: t
			}),
			!e && /* @__PURE__ */ (0, D.jsx)("p", {
				className: "error",
				children: "The project registry is unavailable. Activity cannot be loaded."
			}),
			/* @__PURE__ */ (0, D.jsx)("div", {
				className: "manager-activity-counts",
				"data-manager-activity-counts": "",
				"aria-label": "Project activity counts",
				children: r.summary && /* @__PURE__ */ (0, D.jsx)(ae, { counts: r.summary.counts })
			}),
			/* @__PURE__ */ (0, D.jsx)("h2", {
				className: "manager-activity-projects-title",
				children: "Registered projects"
			}),
			/* @__PURE__ */ (0, D.jsxs)("p", {
				"data-manager-activity-empty": "",
				hidden: !r.summary || r.summary.projects.length !== 0,
				children: [
					"No projects registered yet. ",
					/* @__PURE__ */ (0, D.jsx)("a", {
						className: "text-link",
						href: "/?view=projects",
						"data-snow-navigation": "",
						children: "Choose a host folder in Projects"
					}),
					" to get started. No agent starts until you explicitly activate it."
				]
			}),
			/* @__PURE__ */ (0, D.jsx)("div", {
				className: "manager-activity-cards",
				"data-manager-activity-cards": "",
				children: r.summary?.projects.map((e) => /* @__PURE__ */ (0, D.jsx)(oe, { project: e }, e.project_id))
			}),
			/* @__PURE__ */ (0, D.jsx)("a", {
				className: "button",
				href: "/login",
				"data-manager-activity-login": "",
				hidden: !r.accessEnded,
				children: "Pair this browser again"
			}),
			/* @__PURE__ */ (0, D.jsx)("noscript", { children: /* @__PURE__ */ (0, D.jsxs)("p", { children: [
				"Activity summaries require JavaScript. You can still ",
				/* @__PURE__ */ (0, D.jsx)("a", {
					className: "text-link",
					href: "/?view=projects",
					children: "open Projects"
				}),
				" and review individual conversations. No work has been started."
			] }) })
		]
	});
}
//#endregion
//#region src/organization/model.ts
var ce = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/, le = /^[A-Za-z0-9_-]{1,128}$/, ue = new TextEncoder();
function de(e) {
	return typeof e == "object" && !!e && !Array.isArray(e);
}
function fe(e, t) {
	return typeof e == "string" && e.length <= t && ue.encode(e).length <= t;
}
function pe(e) {
	return typeof e == "number" && Number.isInteger(e) && !Object.is(e, -0) && e >= 0 && e <= 1e4;
}
function me(e) {
	return !(!de(e) || typeof e.id != "string" || !ce.test(e.id) || !fe(e.name, 128) || !e.name || /[\u0000-\u001f\u007f-\u009f]/u.test(e.name) || !fe(e.path, 4096) || !e.path.startsWith("/") || e.path.includes("\0") || typeof e.available != "boolean" || typeof e.pinned != "boolean" || !fe(e.state, 32) || !fe(e.issue, 512));
}
function he(e) {
	return de(e) && typeof e.id == "string" && le.test(e.id) && fe(e.name, 16384) && [...e.name].length <= 4096 && fe(e.updated, 64) && typeof e.pinned == "boolean" && typeof e.archived == "boolean";
}
function ge(e) {
	return new Set(e.map((e) => e.id)).size === e.length;
}
function _e(e, t, n) {
	if (!ce.test(t) || !pe(n)) return null;
	let r = /^\/\?offset=(0|[1-9][0-9]{0,4})&project=([0-9a-f-]{36})&view=organization$/.exec(e);
	if (!r || r[2] !== t) return null;
	let i = Number(r[1]);
	return pe(i) && i > n ? `/?offset=${i}&project=${t}&view=organization` : null;
}
function ve(e) {
	let t = /^\/\?archived_offset=([1-9][0-9]{0,4})&view=organization$/.exec(e);
	if (!t) return null;
	let n = Number(t[1]);
	return pe(n) ? `/?archived_offset=${n}&view=organization` : null;
}
function ye(e) {
	if (!de(e) || typeof e.csrf != "string" || !/^[0-9a-f]{64}$/.test(e.csrf) || !fe(e.error, 4096)) return !1;
	let t = e.organization;
	return t === null || !(!de(t) || !Array.isArray(t.projects) || t.projects.length > 100 || !t.projects.every(me) || !Array.isArray(t.archived) || t.archived.length > 25 || !t.archived.every(me) || !ge([...t.projects, ...t.archived]) || t.project !== null && !me(t.project) || !Array.isArray(t.sessions) || t.sessions.length > 25 || !t.sessions.every(he) || !ge(t.sessions) || !pe(t.offset) || typeof t.live != "boolean" || !fe(t.nextURL, 256) || !fe(t.archivedNextURL, 256) || t.nextURL !== "" && (t.project === null || !_e(t.nextURL, t.project.id, t.offset)) || t.archivedNextURL !== "" && !ve(t.archivedNextURL) || (t.project === null || t.live) && (t.sessions.length !== 0 || t.nextURL !== ""));
}
function be(e) {
	if (!ye(e)) throw Error("Invalid organization page");
	return e;
}
//#endregion
//#region src/organization/OrganizationPage.tsx
function xe({ value: e }) {
	return /* @__PURE__ */ (0, D.jsx)("input", {
		type: "hidden",
		name: "csrf",
		value: e
	});
}
function Se({ children: e }) {
	return /* @__PURE__ */ (0, D.jsx)("span", {
		className: "organization-badge",
		children: e
	});
}
function O({ project: e, csrf: t }) {
	let n = `/projects/${e.id}/organization`, r = `organization-name-${e.id}`;
	return /* @__PURE__ */ (0, D.jsxs)("li", {
		className: "organization-item",
		id: `organization-active-${e.id}`,
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "organization-item-heading",
				children: [
					/* @__PURE__ */ (0, D.jsx)("a", {
						href: `/?view=organization&project=${e.id}`,
						"data-snow-navigation": "",
						children: /* @__PURE__ */ (0, D.jsx)("strong", { children: e.name })
					}),
					e.pinned && /* @__PURE__ */ (0, D.jsx)(Se, { children: "Pinned" }),
					!e.available && /* @__PURE__ */ (0, D.jsx)(Se, { children: e.state })
				]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine mono organization-path",
				children: e.path
			}),
			/* @__PURE__ */ (0, D.jsxs)("form", {
				method: "post",
				action: `${n}/rename`,
				className: "organization-rename",
				children: [
					/* @__PURE__ */ (0, D.jsx)(xe, { value: t }),
					/* @__PURE__ */ (0, D.jsx)("label", {
						htmlFor: r,
						children: "Workspace label"
					}),
					/* @__PURE__ */ (0, D.jsx)("input", {
						id: r,
						name: "name",
						defaultValue: e.name,
						maxLength: 128,
						required: !0
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						className: "button",
						type: "submit",
						children: "Save label"
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "organization-actions",
				children: [/* @__PURE__ */ (0, D.jsxs)("form", {
					method: "post",
					action: `${n}/${e.pinned ? "unpin" : "pin"}`,
					children: [/* @__PURE__ */ (0, D.jsx)(xe, { value: t }), /* @__PURE__ */ (0, D.jsx)("button", {
						className: "quiet",
						type: "submit",
						children: e.pinned ? "Unpin workspace" : "Pin workspace"
					})]
				}), /* @__PURE__ */ (0, D.jsxs)("details", {
					className: "organization-confirm",
					children: [
						/* @__PURE__ */ (0, D.jsx)("summary", { children: "Archive workspace…" }),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "Hide this registration from active workspaces, retaining all files and sessions. Restore it explicitly below; the original folder identity must still match."
						}),
						/* @__PURE__ */ (0, D.jsxs)("form", {
							method: "post",
							action: `${n}/archive`,
							children: [
								/* @__PURE__ */ (0, D.jsx)(xe, { value: t }),
								/* @__PURE__ */ (0, D.jsx)("input", {
									type: "hidden",
									name: "confirm",
									value: "archive"
								}),
								/* @__PURE__ */ (0, D.jsxs)("button", {
									className: "button",
									type: "submit",
									children: ["Confirm archive of ", e.name]
								})
							]
						})
					]
				})]
			})
		]
	});
}
function k({ project: e, csrf: t }) {
	return /* @__PURE__ */ (0, D.jsxs)("li", {
		className: "organization-item",
		id: `organization-archived-${e.id}`,
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "organization-item-heading",
				children: [
					/* @__PURE__ */ (0, D.jsx)("strong", { children: e.name }),
					/* @__PURE__ */ (0, D.jsx)(Se, { children: "Archived" }),
					e.pinned && /* @__PURE__ */ (0, D.jsx)(Se, { children: "Pinned" })
				]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine mono organization-path",
				children: e.path
			}),
			!e.available && /* @__PURE__ */ (0, D.jsxs)("p", {
				className: "fine",
				children: [e.issue, " Restore requires the original folder."]
			}),
			/* @__PURE__ */ (0, D.jsxs)("form", {
				method: "post",
				action: `/projects/${e.id}/organization/restore`,
				children: [
					/* @__PURE__ */ (0, D.jsx)(xe, { value: t }),
					/* @__PURE__ */ (0, D.jsx)("input", {
						type: "hidden",
						name: "confirm",
						value: "restore"
					}),
					/* @__PURE__ */ (0, D.jsxs)("button", {
						className: "button",
						type: "submit",
						disabled: !e.available,
						children: ["Restore ", e.name]
					})
				]
			})
		]
	});
}
function Ce({ csrf: e, session: t, offset: n }) {
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsx)(xe, { value: e }),
		/* @__PURE__ */ (0, D.jsx)("input", {
			type: "hidden",
			name: "session_id",
			value: t.id
		}),
		/* @__PURE__ */ (0, D.jsx)("input", {
			type: "hidden",
			name: "offset",
			value: n
		})
	] });
}
function we({ session: e, project: t, csrf: n, offset: r, hidden: i }) {
	let a = `/projects/${t.id}/sessions/organization`, o = {
		csrf: n,
		session: e,
		offset: r
	};
	return /* @__PURE__ */ (0, D.jsxs)("li", {
		className: "organization-item",
		id: `organization-session-${t.id}-${e.id}`,
		"data-organization-session": "",
		"data-archived": String(e.archived),
		hidden: i,
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "organization-item-heading",
				children: [
					/* @__PURE__ */ (0, D.jsx)("a", {
						href: `/?view=projects&project=${t.id}&session=${e.id}`,
						"data-snow-navigation": "",
						"data-organization-title": "",
						children: /* @__PURE__ */ (0, D.jsx)("strong", { children: e.name || "Untitled session" })
					}),
					e.pinned && /* @__PURE__ */ (0, D.jsx)(Se, { children: "Pinned" }),
					e.archived && /* @__PURE__ */ (0, D.jsx)(Se, { children: "Archived" })
				]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				children: e.updated
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "organization-actions",
				children: [/* @__PURE__ */ (0, D.jsxs)("form", {
					method: "post",
					action: `${a}/${e.pinned ? "unpin" : "pin"}`,
					children: [/* @__PURE__ */ (0, D.jsx)(Ce, { ...o }), /* @__PURE__ */ (0, D.jsx)("button", {
						className: "quiet",
						type: "submit",
						children: e.pinned ? "Unpin conversation" : "Pin conversation"
					})]
				}), e.archived ? /* @__PURE__ */ (0, D.jsxs)("form", {
					method: "post",
					action: `${a}/restore`,
					children: [
						/* @__PURE__ */ (0, D.jsx)(Ce, { ...o }),
						/* @__PURE__ */ (0, D.jsx)("input", {
							type: "hidden",
							name: "confirm",
							value: "restore"
						}),
						/* @__PURE__ */ (0, D.jsx)("button", {
							className: "button",
							type: "submit",
							children: "Restore conversation"
						})
					]
				}) : /* @__PURE__ */ (0, D.jsxs)("form", {
					method: "post",
					action: `${a}/archive`,
					children: [
						/* @__PURE__ */ (0, D.jsx)(Ce, { ...o }),
						/* @__PURE__ */ (0, D.jsx)("input", {
							type: "hidden",
							name: "confirm",
							value: "archive"
						}),
						/* @__PURE__ */ (0, D.jsx)("button", {
							className: "button",
							type: "submit",
							title: "Archive in the manager only. Saved history is kept; Restore reverses this.",
							children: "Archive conversation"
						})
					]
				})]
			})
		]
	});
}
function Te({ project: e, sessions: t, offset: n, nextURL: r, csrf: i }) {
	let [a, o] = (0, l.useState)(""), [s, c] = (0, l.useState)(!0), u = a.trim().toLocaleLowerCase(), d = (e) => (s || !e.archived) && (e.name || "Untitled session").toLocaleLowerCase().includes(u), f = t.filter(d).length, p = _e(r, e.id, n);
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsx)("p", {
			className: "fine",
			children: "This is a bounded, read-only catalog page, not a complete inventory. Live, locked or unsupported sessions may be absent. Pins and archives are manager-only metadata; original conversation IDs and transcripts stay unchanged."
		}),
		/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "organization-filters",
			children: [/* @__PURE__ */ (0, D.jsxs)("label", {
				htmlFor: "organization-title-filter",
				children: ["Search titles on this loaded page only", /* @__PURE__ */ (0, D.jsx)("input", {
					id: "organization-title-filter",
					type: "search",
					maxLength: 128,
					autoComplete: "off",
					"data-organization-filter": "",
					"aria-describedby": "organization-search-help",
					value: a,
					onChange: (e) => o(e.currentTarget.value)
				})]
			}), /* @__PURE__ */ (0, D.jsxs)("label", {
				className: "organization-checkbox",
				children: [
					/* @__PURE__ */ (0, D.jsx)("input", {
						type: "checkbox",
						checked: s,
						"data-organization-archived": "",
						onChange: (e) => c(e.currentTarget.checked)
					}),
					" ",
					"Show archived conversations on this loaded page"
				]
			})]
		}),
		/* @__PURE__ */ (0, D.jsx)("p", {
			className: "fine",
			id: "organization-search-help",
			children: "Loaded-page search does not search other pages, transcript contents or every saved session."
		}),
		/* @__PURE__ */ (0, D.jsxs)("p", {
			className: "fine",
			role: "status",
			"aria-live": "polite",
			"data-organization-status": "",
			children: [
				f,
				" of ",
				t.length,
				" conversations shown on this loaded page. Other pages are not searched."
			]
		}),
		/* @__PURE__ */ (0, D.jsxs)("ul", {
			className: "organization-list",
			children: [t.map((t) => /* @__PURE__ */ (0, D.jsx)(we, {
				session: t,
				project: e,
				csrf: i,
				offset: n,
				hidden: !d(t)
			}, t.id)), t.length === 0 && /* @__PURE__ */ (0, D.jsx)("li", {
				className: "fine",
				children: "No supported saved conversations on this page."
			})]
		}),
		p && /* @__PURE__ */ (0, D.jsx)("a", {
			className: "button",
			href: p,
			"data-snow-navigation": "",
			children: "Next catalog page →"
		})
	] });
}
function Ee({ csrf: e, error: t, organization: n }) {
	let r = n && ve(n.archivedNextURL);
	return /* @__PURE__ */ (0, D.jsxs)("section", {
		className: "organization-panel",
		"aria-labelledby": "organization-heading",
		children: [
			/* @__PURE__ */ (0, D.jsxs)("header", {
				className: "workspace-heading",
				children: [/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("h1", {
					id: "organization-heading",
					children: "Organize workspaces"
				}), /* @__PURE__ */ (0, D.jsx)("p", {
					className: "fine",
					children: "Manager labels, pins and archives only. Nothing here starts an agent or deletes project files or saved conversations."
				})] }), /* @__PURE__ */ (0, D.jsx)("a", {
					className: "button",
					href: "/?view=projects",
					"data-snow-navigation": "",
					children: "Back to workspaces"
				})]
			}),
			t && /* @__PURE__ */ (0, D.jsx)("p", {
				className: "error",
				role: "alert",
				children: t
			}),
			n && /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "organization-grid",
				children: [/* @__PURE__ */ (0, D.jsxs)("section", {
					className: "organization-card",
					"aria-labelledby": "organization-active-heading",
					children: [
						/* @__PURE__ */ (0, D.jsx)("h2", {
							id: "organization-active-heading",
							children: "Active registrations"
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "Up to 100 registrations. Close a live conversation before archiving its workspace. Unpinning does not hide work."
						}),
						/* @__PURE__ */ (0, D.jsxs)("ul", {
							className: "organization-list",
							children: [n.projects.map((t) => /* @__PURE__ */ (0, D.jsx)(O, {
								project: t,
								csrf: e
							}, t.id)), n.projects.length === 0 && /* @__PURE__ */ (0, D.jsx)("li", {
								className: "fine",
								children: "No active registrations."
							})]
						})
					]
				}), /* @__PURE__ */ (0, D.jsxs)("section", {
					className: "organization-card",
					"aria-labelledby": "organization-archive-heading",
					children: [
						/* @__PURE__ */ (0, D.jsx)("h2", {
							id: "organization-archive-heading",
							children: "Archived registrations"
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "Includes registrations previously removed from the manager. At most 25 entries per page; bounded navigation ends at offset 10,000. Restore preserves the exact registration ID and never activates a conversation."
						}),
						/* @__PURE__ */ (0, D.jsxs)("ul", {
							className: "organization-list",
							children: [n.archived.map((t) => /* @__PURE__ */ (0, D.jsx)(k, {
								project: t,
								csrf: e
							}, t.id)), n.archived.length === 0 && /* @__PURE__ */ (0, D.jsx)("li", {
								className: "fine",
								children: "No archived registrations on this page."
							})]
						}),
						r && /* @__PURE__ */ (0, D.jsx)("a", {
							className: "button",
							href: r,
							"data-snow-navigation": "",
							children: "Next archived registrations →"
						})
					]
				})]
			}), n.project ? /* @__PURE__ */ (0, D.jsxs)("section", {
				className: "organization-card organization-sessions",
				"aria-labelledby": "organization-sessions-heading",
				"data-organization-sessions": "",
				"data-organization-ready": "true",
				children: [/* @__PURE__ */ (0, D.jsxs)("h2", {
					id: "organization-sessions-heading",
					children: ["Saved conversations · ", n.project.name]
				}), n.live ? /* @__PURE__ */ (0, D.jsxs)("p", {
					className: "notice",
					children: [
						"This workspace has a live conversation.",
						" ",
						/* @__PURE__ */ (0, D.jsx)("a", {
							href: `/?view=projects&project=${n.project.id}`,
							"data-snow-navigation": "",
							children: "Return to live work"
						}),
						" ",
						"and close it before organizing saved conversations. Live work is never hidden by an archive."
					]
				}) : /* @__PURE__ */ (0, D.jsx)(Te, {
					project: n.project,
					sessions: n.sessions,
					offset: n.offset,
					nextURL: n.nextURL,
					csrf: e
				}, `${n.project.id}:${n.offset}`)]
			}) : /* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				children: "Select an active workspace above to organize a loaded page of its saved conversations."
			})] })
		]
	});
}
var De = 15e3;
function A(e) {
	return typeof e == "object" && !!e && !Array.isArray(e);
}
function Oe(e) {
	return typeof e == "string" && /^browser_[a-f0-9]{32}$/.test(e);
}
function ke(e) {
	if (typeof e != "string" || e.length > 64) return !1;
	let t = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.\d{1,9})?(?:Z|[+-](\d{2}):(\d{2}))$/.exec(e);
	if (!t || !Number.isFinite(Date.parse(e))) return !1;
	let [, n, r, i, a, o, s, c, l] = t, u = Number(n), d = Number(r), f = Number(i), p = [
		31,
		u % 4 == 0 && (u % 100 != 0 || u % 400 == 0) ? 29 : 28,
		31,
		30,
		31,
		30,
		31,
		31,
		30,
		31,
		30,
		31
	];
	return d >= 1 && d <= 12 && f >= 1 && f <= p[d - 1] && Number(a) <= 23 && Number(o) <= 59 && Number(s) <= 59 && (c === void 0 || Number(c) <= 23 && Number(l) <= 59);
}
function Ae(e) {
	if (!A(e) || e.limit !== 8 || !Array.isArray(e.browsers) || e.browsers.length > 8) throw Error("inventory");
	let t = /* @__PURE__ */ new Set(), n = 0;
	return e.browsers.map((e) => {
		if (!A(e) || !Oe(e.id) || t.has(e.id) || typeof e.label != "string" || e.label.length === 0 || e.label.trim() !== e.label || new TextEncoder().encode(e.label).byteLength > 80 || /[\p{Cc}\p{Cf}\p{Cs}]/u.test(e.label) || typeof e.current != "boolean" || !ke(e.created) || !ke(e.last_used) || !ke(e.expires) || Date.parse(e.last_used) < Date.parse(e.created) || Date.parse(e.expires) <= Date.parse(e.created)) throw Error("browser");
		if (t.add(e.id), e.current && ++n > 1) throw Error("current browser");
		return {
			id: e.id,
			label: e.label,
			current: e.current,
			created: e.created,
			last_used: e.last_used,
			expires: e.expires
		};
	});
}
function je(e, t) {
	if (!Oe(t) || !A(e) || e.revoked_id !== t || typeof e.signed_out != "boolean") throw Error("receipt");
	return {
		revoked_id: t,
		signed_out: e.signed_out
	};
}
async function Me(e, t) {
	t.throwIfAborted();
	let n = e.headers.get("Content-Length");
	if (n !== null && (!/^\d+$/.test(n) || Number(n) > 16384)) throw Error("size");
	if (e.headers.get("Content-Type")?.split(";", 1)[0]?.trim().toLowerCase() !== "application/json" || !e.body) throw Error("body");
	let r = e.body.getReader(), i = [], a = 0, o = () => {
		r.cancel().catch(() => {});
	};
	t.addEventListener("abort", o, { once: !0 });
	try {
		for (;;) {
			t.throwIfAborted();
			let { done: e, value: n } = await r.read();
			if (t.throwIfAborted(), e) break;
			if (a += n.byteLength, a > 16384) throw Error("size");
			i.push(n);
		}
		let e = new Uint8Array(a), n = 0;
		for (let t of i) e.set(t, n), n += t.byteLength;
		return JSON.parse(new TextDecoder("utf-8", { fatal: !0 }).decode(e));
	} finally {
		t.removeEventListener("abort", o), r.cancel().catch(() => {}), r.releaseLock();
	}
}
//#endregion
//#region src/browser-access/useBrowserInventory.ts
var Ne = {
	browsers: [],
	selected: null,
	busy: !1,
	ready: !1,
	accessEnded: !1,
	status: "Loading paired browsers…",
	focus: null
}, Pe = "Could not confirm revocation. Access storage may be unavailable. Refresh before trying again; this request will not be replayed.", Fe = "snow:browser-access-ended", Ie = {
	refresh() {},
	select() {},
	cancel() {},
	revoke() {}
};
function Le(e, t) {
	let [n, r] = (0, l.useState)(Ne), i = (0, l.useRef)(Ie);
	return (0, l.useLayoutEffect)(() => {
		let n = !1, a = !1, o = !1, s = 0, c = null, l = [], u = !1, d = !1, f = null, p, m = null, h = () => !n && !a && !o && !!e.current?.isConnected, g = (e) => h() && s === e, _ = () => {
			++s;
			let e = f;
			f = null, clearTimeout(p), e?.abort();
		}, v = () => {
			c = null;
		}, y = (e = "Browser access ended. Pair this browser again; host work was not stopped.") => {
			n || a || (a = !0, _(), v(), l = [], u = !1, r({
				...Ne,
				accessEnded: !0,
				status: e
			}));
		}, b = (e = !1) => {
			y(e ? "This browser has been revoked. Opening the pairing page…" : void 0), document.dispatchEvent(new Event(Fe)), window.location.assign("/login");
		}, x = async (e) => {
			if (!h() || f || e && (!u || c !== e)) return;
			v();
			let n = ++s, i = new AbortController();
			f = i, u = !1, r((t) => ({
				...t,
				selected: null,
				busy: !0,
				ready: !1,
				focus: null,
				status: e ? "Revoking selected browser…" : "Loading paired browsers…"
			})), p = setTimeout(() => i.abort(), De);
			let a;
			try {
				if (a = await fetch(e ? `/access/browsers/${encodeURIComponent(e.id)}/revoke` : "/access/browsers", {
					method: e ? "POST" : "GET",
					credentials: "same-origin",
					cache: "no-store",
					redirect: "error",
					signal: i.signal,
					headers: e ? {
						Accept: "application/json",
						"Content-Type": "application/x-www-form-urlencoded"
					} : { Accept: "application/json" },
					...e ? { body: new URLSearchParams({
						csrf: t,
						confirm: "revoke"
					}) } : {}
				}), !g(n)) return;
				if (i.signal.throwIfAborted(), a.status === 401) {
					b();
					return;
				}
				if (e && a.status === 404) {
					r((e) => ({
						...e,
						status: "That browser is no longer paired. Refresh the inventory before choosing another browser."
					}));
					return;
				}
				if (!a.ok) throw Error("request");
				let o = await Me(a, i.signal);
				if (!g(n)) return;
				if (i.signal.throwIfAborted(), e) {
					if (je(o, e.id).signed_out) {
						b(!0);
						return;
					}
					l = l.filter((t) => t.id !== e.id), u = !0, r((e) => ({
						...e,
						browsers: l,
						ready: u,
						status: "Browser revoked. Other browsers remain paired. Refresh to see the updated inventory."
					}));
				} else l = Ae(o), u = !0, r((e) => ({
					...e,
					browsers: l,
					ready: u,
					status: `${l.length} of 8 browser slots used.`
				}));
			} catch {
				if (!g(n)) return;
				e || (l = []), r((t) => ({
					...t,
					browsers: l,
					selected: null,
					ready: !1,
					status: e ? Pe : "Unable to load browser access. Refresh to retry; no access was changed."
				}));
			} finally {
				a?.body?.cancel().catch(() => {}), g(n) && (clearTimeout(p), f = null, r((e) => ({
					...e,
					busy: !1
				})));
			}
		}, S = () => {
			let e = !!f && d;
			_(), v(), u = !1, r((t) => ({
				...t,
				selected: null,
				busy: !1,
				ready: !1,
				focus: null,
				status: e ? Pe : "Browser inventory paused. Refresh before choosing a browser; no request will be replayed."
			}));
		};
		i.current = {
			refresh() {
				h() && !f && (d = !1, x(null));
			},
			select(e) {
				h() && !f && u && l.includes(e) && (c = e, r((t) => ({
					...t,
					selected: e,
					focus: "cancel"
				})));
			},
			cancel() {
				h() && !f && (_(), v(), r((e) => ({
					...e,
					selected: null,
					focus: "refresh"
				})));
			},
			revoke() {
				c && h() && !f && (d = !0, x(c));
			}
		};
		let ee = () => y(), C = (e) => {
			if (!(e.target instanceof HTMLFormElement)) return;
			let t = new URL(e.target.action, window.location.href);
			t.origin === window.location.origin && ["/logout", "/access/revoke-all"].includes(t.pathname) && y();
		}, w = (t) => {
			let n = t.detail || {};
			if (["/logout", "/access/revoke-all"].includes(n.requestConfig?.path || "")) {
				y();
				return;
			}
			n.target instanceof Element && e.current && n.target.contains(e.current) && (m = n.target, o = !0, S());
		}, te = (e) => {
			let t = e.detail?.target;
			m && (!(t instanceof Element) || t === m) && (m = null, o = !1);
		}, T = () => {
			o = !0, S();
		}, ne = () => {
			o = !1;
		}, E = new MutationObserver(() => {
			!n && !e.current?.isConnected && (S(), n = !0);
		});
		return E.observe(document.documentElement, {
			childList: !0,
			subtree: !0
		}), document.addEventListener("submit", C, !0), document.addEventListener(Fe, ee), document.addEventListener("snow:navigation-start", w), document.addEventListener("snow:navigation-before-swap", w), document.addEventListener("snow:navigation-end", te), document.addEventListener("snow:navigation-after-swap", te), window.addEventListener("pagehide", T), window.addEventListener("pageshow", ne), r(Ne), x(null), () => {
			n = !0, _(), i.current = Ie, E.disconnect(), document.removeEventListener("submit", C, !0), document.removeEventListener(Fe, ee), document.removeEventListener("snow:navigation-start", w), document.removeEventListener("snow:navigation-before-swap", w), document.removeEventListener("snow:navigation-end", te), document.removeEventListener("snow:navigation-after-swap", te), window.removeEventListener("pagehide", T), window.removeEventListener("pageshow", ne);
		};
	}, [e, t]), {
		...n,
		refresh: () => i.current.refresh(),
		select: (e) => i.current.select(e),
		cancel: () => i.current.cancel(),
		revoke: () => i.current.revoke()
	};
}
//#endregion
//#region src/browser-access/BrowserInventory.tsx
function Re({ csrf: e }) {
	let t = (0, l.useRef)(null), n = (0, l.useRef)(null), r = (0, l.useRef)(null), i = Le(t, e);
	(0, l.useLayoutEffect)(() => {
		!t.current?.isConnected || i.busy || i.accessEnded || (i.focus === "cancel" && i.selected && n.current?.focus(), i.focus === "refresh" && r.current?.focus());
	}, [
		i.focus,
		i.selected,
		i.busy,
		i.accessEnded
	]);
	let a = i.busy || i.accessEnded, o = i.selected;
	return /* @__PURE__ */ (0, D.jsxs)("section", {
		ref: t,
		className: "settings-panel browser-inventory",
		"data-browser-inventory": "",
		"aria-label": "Paired browsers",
		"aria-busy": i.busy,
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "browser-inventory-heading",
				children: [/* @__PURE__ */ (0, D.jsx)("h2", { children: "Paired browsers" }), /* @__PURE__ */ (0, D.jsx)("button", {
					ref: r,
					className: "button quiet",
					type: "button",
					"data-browser-refresh": "",
					disabled: a,
					onClick: i.refresh,
					children: "Refresh"
				})]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", { children: "Revoke one browser without signing out the others. Labels are approximate browser-family hints, not verified device identities. Use the reference to distinguish similar browsers." }),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				children: "Created and last seen times are shown in your local time. Last seen is approximate after a restart: activity is saved when browser access changes. Access expires at most 30 days after pairing. Live event streams recheck access every 5 seconds; revocation does not stop a running agent."
			}),
			/* @__PURE__ */ (0, D.jsx)("input", {
				type: "hidden",
				"data-browser-csrf": "",
				value: e,
				readOnly: !0
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				"data-browser-status": "",
				role: "status",
				"aria-live": "polite",
				children: i.status
			}),
			/* @__PURE__ */ (0, D.jsx)("ul", {
				className: "browser-inventory-list",
				"data-browser-list": "",
				children: i.browsers.map((e) => /* @__PURE__ */ (0, D.jsxs)("li", {
					"data-browser-id": e.id,
					children: [/* @__PURE__ */ (0, D.jsxs)("div", { children: [
						/* @__PURE__ */ (0, D.jsxs)("strong", { children: [e.label, e.current ? " · This browser" : ""] }),
						/* @__PURE__ */ (0, D.jsxs)("small", { children: ["Reference ", e.id.slice(-8)] }),
						/* @__PURE__ */ (0, D.jsxs)("small", { children: [
							"Created ",
							new Date(e.created).toLocaleString(),
							" · Last seen ",
							new Date(e.last_used).toLocaleString(),
							" · Expires ",
							new Date(e.expires).toLocaleString()
						] })
					] }), /* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "button danger",
						disabled: a || !i.ready,
						onClick: () => i.select(e),
						"aria-label": `Revoke ${e.label}, reference ${e.id.slice(-8)}${e.current ? ", this browser" : ""}`,
						children: e.current ? "Revoke this browser…" : "Revoke browser…"
					})]
				}, e.id))
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "browser-revoke-confirm",
				"data-browser-confirm": "",
				hidden: !o,
				children: [
					/* @__PURE__ */ (0, D.jsx)("h3", { children: "Revoke this browser?" }),
					/* @__PURE__ */ (0, D.jsx)("p", {
						"data-browser-confirm-description": "",
						children: o && `${o.label} · reference ${o.id.slice(-8)}. ${o.current ? "This is your current browser. Confirming signs you out and opens the pairing page." : "This browser will need to pair again."}`
					}),
					/* @__PURE__ */ (0, D.jsx)("p", { children: "This removes only the selected browser's access. The pairing code is unchanged; anyone holding it can pair again. Rotate the pairing code separately if needed." }),
					/* @__PURE__ */ (0, D.jsxs)("div", {
						className: "browser-inventory-actions",
						children: [/* @__PURE__ */ (0, D.jsx)("button", {
							ref: n,
							className: "button",
							type: "button",
							"data-browser-cancel": "",
							disabled: a,
							onClick: i.cancel,
							children: "Cancel"
						}), /* @__PURE__ */ (0, D.jsx)("button", {
							className: "button danger",
							type: "button",
							"data-browser-revoke": "",
							disabled: a || !o || !i.ready,
							onClick: i.revoke,
							children: "Confirm revoke browser"
						})]
					})
				]
			}),
			i.accessEnded && /* @__PURE__ */ (0, D.jsx)("p", { children: /* @__PURE__ */ (0, D.jsx)("a", {
				className: "button",
				href: "/login",
				children: "Pair this browser again"
			}) }),
			/* @__PURE__ */ (0, D.jsx)("noscript", { children: /* @__PURE__ */ (0, D.jsx)("p", { children: "Enable JavaScript to list and individually revoke browsers. The existing Sign out and Revoke all controls remain separate." }) })
		]
	});
}
//#endregion
//#region src/host-settings/model.ts
var ze = {
	thinking: [
		"off",
		"minimal",
		"low",
		"medium",
		"high",
		"xhigh",
		"max",
		"ultra"
	],
	reasoning_summary: [
		"off",
		"auto",
		"concise",
		"detailed"
	],
	text_verbosity: [
		"low",
		"medium",
		"high"
	]
};
function Be(e) {
	return typeof e == "object" && !!e && !Array.isArray(e);
}
function Ve(e) {
	return typeof e == "string" && /^[a-z0-9][a-z0-9_.-]{0,63}$/.test(e);
}
function He(e) {
	return e === "global" ? [
		"provider_model",
		"thinking",
		"reasoning_summary",
		"text_verbosity"
	] : ["provider_model", "thinking"];
}
function Ue(e, t) {
	return Be(e) && (Ve(e.provider) || t && e.provider === "") && typeof e.model == "string" && e.model.length <= 256;
}
function We(e, t, n) {
	return Be(e) && (e.source === "builtin" || e.source === "global" || t === "project" && e.source === "project") && n(e.effective, !1) && (e.explicit === null || n(e.explicit, !0));
}
function Ge(e, t) {
	let n = () => /* @__PURE__ */ Error("Invalid host defaults projection");
	if (!Be(e) || e.scope !== t.scope || (e.project_id ?? "") !== t.project || typeof e.revision != "string" || !e.revision || e.revision.length > 128 || e.applies_to !== "future_runtime" || e.availability !== "not_network_verified") throw n();
	let r = e[t.scope];
	if (!Be(r) || !We(r.provider_model, t.scope, Ue)) throw n();
	let i = {
		provider_model: r.provider_model,
		thinking: {
			explicit: null,
			effective: "off",
			source: "builtin"
		}
	};
	for (let e of He(t.scope)) {
		if (e === "provider_model") continue;
		let a = r[e];
		if (!We(a, t.scope, (t) => typeof t == "string" && ze[e].includes(t))) throw n();
		i[e] = a;
	}
	return {
		...t,
		revision: e.revision,
		group: i
	};
}
function Ke(e) {
	return He(e.scope).map((t) => {
		if (t === "provider_model") {
			let n = e.group.provider_model, r = n.explicit ?? n.effective;
			return {
				name: t,
				op: "unchanged",
				saved: n,
				value: {
					provider: r.provider || n.effective.provider,
					model: r.model
				}
			};
		}
		let n = e.group[t];
		if (!n) throw Error("Missing host defaults field");
		return {
			name: t,
			op: "unchanged",
			saved: n,
			value: n.explicit ?? n.effective
		};
	});
}
function qe(e, t, n) {
	let r = new URLSearchParams({
		csrf: e,
		scope: t.scope,
		expected_revision: t.revision
	});
	t.project && r.set("project", t.project);
	let i = !1;
	for (let e of n) e.op !== "unchanged" && (i = !0, r.set(`${e.name}_op`, e.op), e.op === "set" && (e.name === "provider_model" ? (r.set("provider", e.value.provider), r.set("model", e.value.model)) : r.set(e.name, e.value)));
	return i ? r : null;
}
function Je(e) {
	if (!Be(e) || e.checked_locally !== !0 || !Array.isArray(e.providers) || e.providers.length > 128) throw Error("Invalid provider status");
	let t = /* @__PURE__ */ new Set();
	return e.providers.map((e) => {
		if (!Be(e) || !Ve(e.provider_id) || t.has(e.provider_id) || e.checked_locally !== !0) throw Error("Invalid provider status");
		let { state: n, reason: r } = e;
		if (typeof r != "string" || !(n === "configured" && ["credential_present", "anonymous_access"].includes(r) || n === "expired" && r === "credential_expired" || n === "unavailable" && [
			"credential_missing",
			"credential_invalid",
			"auth_store_unavailable"
		].includes(r))) throw Error("Invalid provider status");
		return t.add(e.provider_id), {
			provider_id: e.provider_id,
			state: n,
			reason: r,
			checked_locally: !0
		};
	});
}
async function Ye(e, t) {
	t.throwIfAborted();
	let n = e.headers.get("Content-Length");
	if (n !== null && (!/^\d+$/.test(n) || Number(n) > 65536)) throw Error("size");
	if (e.headers.get("Content-Type")?.split(";", 1)[0]?.trim().toLowerCase() !== "application/json") throw Error("type");
	if (!e.body) throw Error("body");
	let r = e.body.getReader(), i = new TextDecoder("utf-8", { fatal: !0 }), a = 0, o = "", s = () => {}, c = new Promise((e, t) => {
		s = t;
	}), l = () => {
		s(t.reason), r.cancel().catch(() => {});
	};
	t.addEventListener("abort", l, { once: !0 });
	try {
		for (;;) {
			t.throwIfAborted();
			let { done: e, value: n } = await Promise.race([r.read(), c]);
			if (t.throwIfAborted(), e) break;
			if (a += n.byteLength, a > 65536) throw Error("size");
			o += i.decode(n, { stream: !0 });
		}
		return o += i.decode(), t.throwIfAborted(), JSON.parse(o);
	} finally {
		t.removeEventListener("abort", l), r.cancel().catch(() => {}), r.releaseLock();
	}
}
async function Xe(e, t, n = {}) {
	let r = AbortSignal.any([t, AbortSignal.timeout(6500)]);
	r.throwIfAborted();
	let i = await fetch(e, {
		credentials: "same-origin",
		cache: "no-store",
		redirect: "error",
		...n,
		signal: r
	});
	try {
		if (r.throwIfAborted(), !i.ok || i.redirected) throw Error(i.status === 409 ? "conflict" : "request");
		return await Ye(i, r);
	} catch (e) {
		throw i.body?.cancel().catch(() => {}), e;
	}
}
//#endregion
//#region src/host-settings/HostSettingsPanel.tsx
var Ze = () => ({
	target: {
		scope: "global",
		project: ""
	},
	selectedProject: "",
	loaded: null,
	rows: [],
	writable: !1,
	pending: null,
	status: "Not loaded. Opening General does not read host defaults or start a worker.",
	providerStatus: "",
	providers: []
}), Qe = (e, t) => e.scope === t.scope && e.project === t.project, $e = (e) => `${e.provider || "inherited provider"} / ${e.model || "provider default"}`;
function et({ row: e, locked: t, update: n }) {
	let r = e.name.replaceAll("_", " "), i = e.saved.explicit === null ? "Inherited" : e.name === "provider_model" ? $e(e.saved.explicit) : e.saved.explicit, a = e.name === "provider_model" ? $e(e.saved.effective) : e.saved.effective, o = t || e.op !== "set";
	return /* @__PURE__ */ (0, D.jsxs)("div", {
		className: "host-default-row",
		children: [
			/* @__PURE__ */ (0, D.jsx)("strong", { children: r }),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				children: `Explicit: ${i}. Effective: ${a}. Source: ${e.saved.source}. Availability: not network verified.`
			}),
			/* @__PURE__ */ (0, D.jsxs)("label", { children: [
				r,
				" operation",
				/* @__PURE__ */ (0, D.jsxs)("select", {
					"data-host-edit": "",
					value: e.op,
					disabled: t,
					onChange: (t) => n({
						...e,
						op: t.target.value
					}),
					children: [
						/* @__PURE__ */ (0, D.jsx)("option", {
							value: "unchanged",
							children: "Leave unchanged"
						}),
						/* @__PURE__ */ (0, D.jsx)("option", {
							value: "set",
							children: "Set explicit value"
						}),
						/* @__PURE__ */ (0, D.jsx)("option", {
							value: "reset",
							children: "Reset to inherited default"
						})
					]
				})
			] }),
			e.name === "provider_model" ? /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("label", { children: ["Provider (saved together with model)", /* @__PURE__ */ (0, D.jsx)("input", {
				type: "text",
				"data-host-edit": "",
				value: e.value.provider,
				maxLength: 64,
				autoComplete: "off",
				disabled: o,
				onChange: (t) => n({
					...e,
					value: {
						...e.value,
						provider: t.target.value
					}
				})
			})] }), /* @__PURE__ */ (0, D.jsxs)("label", { children: ["Model ID", /* @__PURE__ */ (0, D.jsx)("input", {
				type: "text",
				"data-host-edit": "",
				value: e.value.model,
				maxLength: 256,
				autoComplete: "off",
				disabled: o,
				onChange: (t) => n({
					...e,
					value: {
						...e.value,
						model: t.target.value
					}
				})
			})] })] }) : /* @__PURE__ */ (0, D.jsxs)("label", { children: [
				r,
				" value",
				/* @__PURE__ */ (0, D.jsx)("select", {
					"data-host-edit": "",
					value: e.value,
					disabled: o,
					onChange: (t) => n({
						...e,
						value: t.target.value
					}),
					children: ze[e.name].map((e) => /* @__PURE__ */ (0, D.jsx)("option", {
						value: e,
						children: e
					}, e))
				})
			] })
		]
	});
}
function tt({ csrf: e, enabled: t, projects: n }) {
	let [r, i] = (0, l.useState)(Ze), [a] = (0, l.useState)(() => ({
		live: !1,
		generation: 0,
		pending: null,
		target: {
			scope: "global",
			project: ""
		}
	})), o = JSON.stringify(n.map((e) => e.id));
	(0, l.useLayoutEffect)(() => (a.live = t, i((e) => ({
		...e,
		pending: null,
		writable: !1
	})), () => {
		a.live = !1, a.generation++;
		let e = a.pending;
		a.pending = null, e?.abort();
	}), [
		a,
		t,
		e,
		o
	]);
	let s = !t || r.pending !== null;
	function c(e, t) {
		a.live && !a.pending && (!e.project || n.some((t) => t.id === e.project)) && (n.some((e) => e.id === t) || (t = ""), a.target = e, a.generation++, i((n) => ({
			...n,
			target: e,
			selectedProject: t,
			loaded: null,
			rows: [],
			writable: !1,
			status: "Not loaded for this scope. Select Load host settings to continue."
		})));
	}
	function u(e) {
		a.live && !a.pending && i((t) => ({
			...t,
			rows: t.rows.map((t) => t.name === e.name ? e : t)
		}));
	}
	function d(e, t) {
		if (!a.live || a.pending || !Qe(t, a.target)) return null;
		let n = new AbortController(), r = ++a.generation;
		a.pending = n, i((t) => ({
			...t,
			pending: e,
			writable: e === "providers" && t.writable
		}));
		let o = () => a.live && a.pending === n && a.generation === r && Qe(a.target, t);
		return {
			signal: n.signal,
			active: o,
			finish: () => {
				o() && (a.pending = null, i((e) => ({
					...e,
					pending: null
				})));
			}
		};
	}
	function f(e) {
		return e.scope === "global" || !!e.project && n.some((t) => t.id === e.project);
	}
	async function p() {
		if (!a.live || a.pending) return;
		let e = r.target;
		if (!f(e)) {
			i((e) => ({
				...e,
				status: "Choose a registered project first."
			}));
			return;
		}
		let t = d("load", e);
		if (!t) return;
		i((e) => ({
			...e,
			status: "Loading host defaults locally…"
		}));
		let n = new URLSearchParams({ scope: e.scope });
		e.project && n.set("project", e.project);
		try {
			let r = await Xe(`/settings/host?${n}`, t.signal, { headers: { Accept: "application/json" } });
			if (!t.active()) return;
			let a = Ge(r, e);
			i((e) => ({
				...e,
				loaded: a,
				rows: Ke(a),
				writable: !0,
				status: "Loaded. Edits apply only to future workers; current workers and new conversations within them are unchanged."
			}));
		} catch {
			t.active() && i((e) => ({
				...e,
				status: "Unable to load. Existing form edits were preserved. Explicitly load again before saving."
			}));
		} finally {
			t.finish();
		}
	}
	async function m() {
		if (!a.live || a.pending || !r.writable || !r.loaded || !Qe(r.target, r.loaded) || !f(r.target)) return;
		let t = r.target, n = qe(e, r.loaded, r.rows);
		if (!n) {
			i((e) => ({
				...e,
				status: "Choose an explicit set or reset operation first."
			}));
			return;
		}
		let o = d("save", t);
		if (o) {
			i((e) => ({
				...e,
				status: "Saving defaults for future workers…"
			}));
			try {
				let e = await Xe("/settings/host", o.signal, {
					method: "POST",
					headers: {
						Accept: "application/json",
						"Content-Type": "application/x-www-form-urlencoded"
					},
					body: n
				});
				if (!o.active()) return;
				let r = Ge(e, t);
				i((e) => ({
					...e,
					loaded: r,
					rows: Ke(r),
					writable: !0,
					status: "Saved for future workers only. Existing workers and their new conversations are unchanged."
				}));
			} catch {
				o.active() && i((e) => ({
					...e,
					status: "Save could not be confirmed; it may have completed. Edits are preserved, not replayed. Explicitly load and review the latest revision before retrying."
				}));
			} finally {
				o.finish();
			}
		}
	}
	async function h() {
		let e = d("providers", r.target);
		if (e) {
			i((e) => ({
				...e,
				providerStatus: "Checking local provider status…"
			}));
			try {
				let t = await Xe("/settings/providers", e.signal, { headers: { Accept: "application/json" } });
				if (!e.active()) return;
				let n = Je(t);
				i((e) => ({
					...e,
					providers: n,
					providerStatus: "Checked locally. No login, refresh, or provider network request was made."
				}));
			} catch {
				e.active() && i((e) => ({
					...e,
					providerStatus: "Status unavailable. Explicitly load again to retry; no login was attempted."
				}));
			} finally {
				e.finish();
			}
		}
	}
	return /* @__PURE__ */ (0, D.jsxs)("section", {
		className: "settings-group host-settings",
		"data-host-settings": "",
		"data-enabled": String(t),
		"aria-busy": r.pending !== null,
		children: [
			/* @__PURE__ */ (0, D.jsx)("h4", { children: "Current session" }),
			/* @__PURE__ */ (0, D.jsx)("p", { children: "Host defaults below do not change the active worker or a new conversation created inside that worker. Use the conversation’s reasoning controls for current-session settings." }),
			/* @__PURE__ */ (0, D.jsx)("h4", { children: "Host defaults" }),
			/* @__PURE__ */ (0, D.jsx)("p", { children: "Changes apply only to future worker activation. Loading uses a short-lived, runtime-free host control worker, not an agent. Availability is checked locally, never verified with a provider network request." }),
			/* @__PURE__ */ (0, D.jsx)("input", {
				type: "hidden",
				"data-host-csrf": "",
				value: e
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "host-settings-target",
				children: [
					/* @__PURE__ */ (0, D.jsxs)("label", { children: ["Defaults scope ", /* @__PURE__ */ (0, D.jsxs)("select", {
						"data-host-scope": "",
						disabled: s,
						value: r.target.scope,
						onChange: (e) => {
							let t = e.target.value;
							(t === "global" || t === "project") && c({
								scope: t,
								project: t === "project" ? r.selectedProject : ""
							}, r.selectedProject);
						},
						children: [/* @__PURE__ */ (0, D.jsx)("option", {
							value: "global",
							children: "Global defaults"
						}), /* @__PURE__ */ (0, D.jsx)("option", {
							value: "project",
							children: "Project defaults"
						})]
					})] }),
					/* @__PURE__ */ (0, D.jsxs)("label", {
						"data-host-project-label": "",
						hidden: r.target.scope !== "project",
						children: ["Registered project ", /* @__PURE__ */ (0, D.jsxs)("select", {
							"data-host-project": "",
							disabled: s,
							value: r.selectedProject,
							onChange: (e) => c({
								scope: r.target.scope,
								project: r.target.scope === "project" ? e.target.value : ""
							}, e.target.value),
							children: [/* @__PURE__ */ (0, D.jsx)("option", {
								value: "",
								children: "Choose a registered project"
							}), n.map((e) => /* @__PURE__ */ (0, D.jsx)("option", {
								value: e.id,
								children: e.name
							}, e.id))]
						})]
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "button",
						"data-host-load": "",
						disabled: s,
						onClick: () => {
							p();
						},
						children: "Load host settings"
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				"data-host-status": "",
				role: "status",
				"aria-live": "polite",
				children: r.status
			}),
			/* @__PURE__ */ (0, D.jsxs)("form", {
				"data-host-form": "",
				hidden: !r.loaded,
				onSubmit: (e) => {
					e.preventDefault(), m();
				},
				children: [/* @__PURE__ */ (0, D.jsx)("div", {
					"data-host-fields": "",
					children: r.rows.map((e) => /* @__PURE__ */ (0, D.jsx)(et, {
						row: e,
						locked: s,
						update: u
					}, e.name))
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "submit",
					className: "button primary",
					"data-host-save": "",
					disabled: s || !r.writable,
					children: "Save defaults for future workers"
				})]
			}),
			/* @__PURE__ */ (0, D.jsx)("h4", { children: "Provider status" }),
			/* @__PURE__ */ (0, D.jsx)("p", { children: "Local credential presence or expiry does not establish network access or model availability." }),
			/* @__PURE__ */ (0, D.jsx)("button", {
				type: "button",
				className: "button",
				"data-host-providers-load": "",
				disabled: s,
				onClick: () => {
					h();
				},
				children: "Load provider status"
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				"data-host-providers-status": "",
				className: "fine",
				role: "status",
				children: r.providerStatus
			}),
			/* @__PURE__ */ (0, D.jsx)("ul", {
				"data-host-providers-list": "",
				children: r.providers.map((e) => /* @__PURE__ */ (0, D.jsx)("li", { children: `${e.provider_id}: ${e.state} — ${e.reason === "anonymous_access" ? "anonymous access; " : ""}local check only, not network verified` }, e.provider_id))
			}),
			/* @__PURE__ */ (0, D.jsx)("h4", { children: "Connect providers on the Snow host" }),
			/* @__PURE__ */ (0, D.jsx)("p", { children: "Do not put passwords or API keys in CLI arguments. In a terminal on the Snow host, run the appropriate interactive login, then check local authentication:" }),
			/* @__PURE__ */ (0, D.jsx)("ul", {
				className: "host-login-instructions",
				children: [
					"snow login opencode-go",
					"snow login chatgpt",
					"snow login openai-compatible",
					"snow auth check"
				].map((e) => /* @__PURE__ */ (0, D.jsx)("li", { children: /* @__PURE__ */ (0, D.jsx)("code", { children: e }) }, e))
			})
		]
	});
}
//#endregion
//#region src/reasoning/model.ts
var nt = {
	thinking: "Thinking",
	reasoning_summary: "Reasoning summary",
	text_verbosity: "Text verbosity"
}, rt = {
	thinking: "thinking_levels",
	reasoning_summary: "reasoning_summaries",
	text_verbosity: "text_verbosities"
}, it = [
	"project_id",
	"instance_id",
	"session_id"
], at = [
	...it,
	"branch_id",
	"tip_id",
	"provider",
	"model",
	"mode",
	"permission_mode",
	"thinking",
	"reasoning_summary",
	"text_verbosity"
], ot = (e) => typeof e == "string" && e.length > 0 && e.length <= 256 && !/[\u0000-\u001f\u007f]/.test(e);
function st(e, t) {
	if (!e || typeof e != "object") return !1;
	let n = e;
	return it.every((e) => n[e] === t[e]) && at.every((e) => e === "tip_id" && n[e] === "" || ot(n[e])) && typeof n.revision == "number" && Number.isSafeInteger(n.revision) && n.revision > 0 && ["default", "plan"].includes(n.mode) && [
		"ask",
		"deny",
		"allow"
	].includes(n.permission_mode) && n.defaults_available === !1 && n.current_session_available === !0 && Object.values(rt).every((e) => n[e] == null || Array.isArray(n[e]) && n[e].length <= 16 && n[e].every(ot) && new Set(n[e]).size === n[e].length);
}
function ct(e, t) {
	return !!e && !!t && e.revision === t.revision && [
		...it,
		"provider",
		"model",
		"mode",
		"permission_mode",
		"thinking"
	].every((n) => e[n] === t[n]);
}
//#endregion
//#region src/reasoning/ReasoningPanel.tsx
function lt({ controller: e }) {
	let t = e.ready(), n = !e.uncertain && ct(e.displayed, e.snapshot) ? e.displayed : null, r = e.uncertain ? "Unverified" : e.snapshot?.thinking || "Unavailable", i = e.inspected?.thinking_levels || [], a = !!e.inspected && !!e.field && e.value !== e.inspected[e.field] && e.inspected[rt[e.field]]?.includes(e.value), o = n ? Object.keys(nt).filter((e) => e !== "thinking" && n[rt[e]]?.length) : [], s = e.field && n?.[rt[e.field]] || [], c = e.error || (e.read ? "Reading authoritative settings and model capabilities…" : e.committing ? "Applying one session-only update…" : ct(e.inspected, e.snapshot) ? e.ui?.safe ? "Current values verified. Changes apply only to this runtime; host and project defaults remain unchanged." : "Controls require an idle connected runtime with no other operation, active goal or queue review." : "Close and reopen Thinking to inspect the current session.");
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [(0, u.createPortal)(/* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("button", {
		type: "button",
		className: "composer-context-button reasoning-trigger",
		"data-reasoning-open": "",
		title: e.committing ? `Updating thinking · Last reported: ${r}` : e.ui?.readable ? `Thinking: ${r}` : `Last reported thinking: ${r} · Controls unavailable`,
		"aria-busy": e.committing,
		"aria-haspopup": "menu",
		"aria-expanded": e.pickerOpen,
		"aria-controls": "reasoning-picker",
		popoverTarget: "reasoning-picker",
		disabled: !e.ui?.readable || e.committing || e.uncertain,
		onClick: (t) => {
			t.preventDefault(), e.open(t.currentTarget);
		},
		onKeyDown: (t) => {
			(t.key === "ArrowDown" || t.key === "ArrowUp") && (t.preventDefault(), e.pickerOpen || e.open(t.currentTarget));
		},
		children: [
			/* @__PURE__ */ (0, D.jsx)("span", { children: "Thinking:" }),
			" ",
			/* @__PURE__ */ (0, D.jsx)("span", {
				"data-reasoning-current": "",
				children: r
			}),
			/* @__PURE__ */ (0, D.jsx)("svg", {
				className: "icon",
				width: "12",
				height: "12",
				viewBox: "0 0 24 24",
				fill: "none",
				stroke: "currentColor",
				strokeWidth: "1.7",
				"aria-hidden": "true",
				children: /* @__PURE__ */ (0, D.jsx)("path", { d: "m7 10 5 5 5-5" })
			})
		]
	}), /* @__PURE__ */ (0, D.jsxs)("div", {
		ref: e.setPicker,
		id: "reasoning-picker",
		className: "reasoning-picker",
		popover: "auto",
		tabIndex: -1,
		"aria-label": "Thinking",
		onKeyDown: e.pickerKey,
		children: [
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "reasoning-picker-heading",
				children: "Thinking"
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine reasoning-picker-notice",
				"data-reasoning-picker-notice": "",
				role: "status",
				children: e.uncertain || e.error || e.read || !t ? c : "Applies to this session only."
			}),
			/* @__PURE__ */ (0, D.jsx)("div", {
				role: "menu",
				"aria-label": "Thinking level",
				"aria-busy": !!e.read || e.committing,
				children: i.map((r) => /* @__PURE__ */ (0, D.jsxs)("button", {
					type: "button",
					role: "menuitemradio",
					"aria-checked": n?.thinking === r,
					"data-reasoning-level": r,
					disabled: !t,
					onClick: () => e.choose(r),
					children: [/* @__PURE__ */ (0, D.jsx)("span", { children: r }), /* @__PURE__ */ (0, D.jsx)("span", {
						"aria-hidden": "true",
						children: n?.thinking === r ? "✓" : ""
					})]
				}, r))
			}),
			!e.read && e.inspected && !i.length ? /* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				children: "This model advertises no adjustable thinking levels."
			}) : null,
			o.length ? /* @__PURE__ */ (0, D.jsx)("div", {
				className: "reasoning-picker-secondary",
				children: /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					"data-reasoning-advanced": "",
					disabled: !!e.read || e.committing || e.uncertain,
					onClick: e.openAdvanced,
					children: "Response settings…"
				})
			}) : null
		]
	})] }), e.launcher), /* @__PURE__ */ (0, D.jsxs)("dialog", {
		ref: e.setDialog,
		id: "reasoning-dialog",
		className: "reasoning-dialog runtime-dialog",
		"aria-labelledby": "reasoning-heading",
		"aria-describedby": "reasoning-boundary",
		onCancel: (t) => {
			t.preventDefault(), e.close();
		},
		onClose: e.cancel,
		onCompositionStart: () => e.composition(!0),
		onCompositionEnd: () => e.composition(!1),
		children: [/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "dialog-heading",
			children: [
				/* @__PURE__ */ (0, D.jsx)("h2", {
					id: "reasoning-heading",
					children: "Response settings"
				}),
				/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-reasoning-refresh": "",
					disabled: !e.ui?.readable || !!e.read || e.committing || e.uncertain,
					onClick: () => void e.inspect(),
					children: "Refresh settings"
				}),
				/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet icon-button",
					"data-reasoning-close": "",
					"aria-label": "Close response settings",
					title: "Close",
					disabled: e.committing,
					onClick: e.close,
					children: /* @__PURE__ */ (0, D.jsx)("svg", {
						className: "icon",
						width: "18",
						height: "18",
						viewBox: "0 0 24 24",
						fill: "none",
						stroke: "currentColor",
						strokeWidth: "1.7",
						strokeLinecap: "round",
						"aria-hidden": "true",
						children: /* @__PURE__ */ (0, D.jsx)("path", { d: "m6 6 12 12M6 18 18 6" })
					})
				})
			]
		}), /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "runtime-dialog-body",
			children: [
				/* @__PURE__ */ (0, D.jsx)("p", {
					id: "reasoning-boundary",
					className: "fine",
					children: "Inspection does not send a prompt. Available choices come from the worker’s model capabilities, not a model-name guess. Settings changes require an idle session without active goals or pending/review queue items."
				}),
				/* @__PURE__ */ (0, D.jsx)("p", {
					"data-reasoning-notice": "",
					className: "fine",
					role: "status",
					"aria-live": "polite",
					children: c
				}),
				/* @__PURE__ */ (0, D.jsxs)("section", {
					"aria-labelledby": "reasoning-current-heading",
					children: [
						/* @__PURE__ */ (0, D.jsx)("h3", {
							id: "reasoning-current-heading",
							children: "Current session — effective values"
						}),
						/* @__PURE__ */ (0, D.jsxs)("dl", {
							className: "reasoning-facts",
							children: [
								/* @__PURE__ */ (0, D.jsx)("dt", { children: "Project / session" }),
								/* @__PURE__ */ (0, D.jsx)("dd", {
									"data-reasoning-identity": "",
									children: n ? `${n.project_id} / ${n.session_id}` : ""
								}),
								/* @__PURE__ */ (0, D.jsx)("dt", { children: "Model / permissions" }),
								/* @__PURE__ */ (0, D.jsx)("dd", {
									"data-reasoning-authority": "",
									children: n ? `${n.provider} / ${n.model} · permissions: ${n.permission_mode}` : ""
								}),
								/* @__PURE__ */ (0, D.jsx)("dt", { children: "Collaboration mode" }),
								/* @__PURE__ */ (0, D.jsx)("dd", {
									"data-reasoning-mode": "",
									children: n?.mode || ""
								}),
								/* @__PURE__ */ (0, D.jsx)("dt", { children: "Reasoning summary" }),
								/* @__PURE__ */ (0, D.jsx)("dd", {
									"data-reasoning-summary": "",
									children: n?.reasoning_summary || ""
								}),
								/* @__PURE__ */ (0, D.jsx)("dt", { children: "Text verbosity" }),
								/* @__PURE__ */ (0, D.jsx)("dd", {
									"data-reasoning-verbosity": "",
									children: n?.text_verbosity || ""
								})
							]
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "These are effective runtime values, not saved defaults. Changes apply only to this runtime: no host or project configuration is written. These overrides are not saved as configuration or new session metadata."
						})
					]
				}),
				/* @__PURE__ */ (0, D.jsxs)("section", {
					"aria-labelledby": "reasoning-session-heading",
					children: [
						/* @__PURE__ */ (0, D.jsx)("h3", {
							id: "reasoning-session-heading",
							children: "Update this session’s runtime"
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "Only the selected preference is changed; other response preferences, model selection, permissions and extension settings are untouched. No prompt or goal starts."
						}),
						/* @__PURE__ */ (0, D.jsx)("label", {
							htmlFor: "reasoning-field",
							children: "Preference"
						}),
						/* @__PURE__ */ (0, D.jsxs)("select", {
							id: "reasoning-field",
							"data-reasoning-field": "",
							disabled: !t,
							value: e.field,
							onChange: (t) => e.selectField(t.currentTarget.value),
							children: [o.map((e) => /* @__PURE__ */ (0, D.jsx)("option", {
								value: e,
								children: nt[e]
							}, e)), n && !o.length ? /* @__PURE__ */ (0, D.jsx)("option", {
								value: "",
								children: "No supported mutable preferences advertised"
							}) : null]
						}),
						/* @__PURE__ */ (0, D.jsx)("label", {
							htmlFor: "reasoning-value",
							children: "New value"
						}),
						/* @__PURE__ */ (0, D.jsx)("select", {
							id: "reasoning-value",
							"data-reasoning-value": "",
							disabled: !t,
							value: e.value,
							onChange: (t) => e.selectValue(t.currentTarget.value),
							children: s.map((e) => /* @__PURE__ */ (0, D.jsx)("option", {
								value: e,
								children: e
							}, e))
						}),
						/* @__PURE__ */ (0, D.jsx)("div", {
							className: "dialog-actions",
							children: /* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "button primary",
								"data-reasoning-confirm": "",
								disabled: !t || !a || !e.inspected?.current_session_available,
								onClick: () => void e.commit(),
								children: "Apply session-only update"
							})
						})
					]
				})
			]
		})]
	})] });
}
//#endregion
//#region src/reasoning/controller.tsx
var ut = null, dt = class {
	api;
	root;
	launcher;
	dialog = null;
	picker = null;
	opener = null;
	pickerOpen = !1;
	pickerLifetime = null;
	snapshot = null;
	ui = null;
	inspected = null;
	displayed = null;
	read = null;
	field = "";
	value = "";
	committing = !1;
	uncertain = !1;
	composing = !1;
	error = "";
	constructor(e, t, n) {
		this.launcher = n, this.api = {
			...e,
			identity: Object.freeze({ ...e.identity })
		}, this.root = (0, d.createRoot)(t);
	}
	current = () => ut === this;
	ready = () => this.current() && !!this.ui?.safe && ct(this.inspected, this.snapshot) && !this.read && !this.committing && !this.uncertain && !this.composing;
	publish = () => {
		this.current() && (0, u.flushSync)(() => this.root.render(/* @__PURE__ */ (0, D.jsx)(lt, { controller: this })));
	};
	setDialog = (e) => {
		this.dialog = e;
	};
	setPicker = (e) => {
		this.picker?.removeEventListener("toggle", this.pickerToggled), this.picker = e, e?.addEventListener("toggle", this.pickerToggled);
	};
	pickerToggled = () => {
		this.current() && this.picker && (this.pickerOpen = this.picker.matches(":popover-open"), this.pickerOpen || (this.pickerLifetime?.abort(), this.pickerLifetime = null, this.dialog?.open || this.cancel()), this.publish());
	};
	positionPicker = () => {
		if (!this.picker || !this.opener) return;
		let e = this.opener.getBoundingClientRect(), t = Math.min(288, window.innerWidth - 16), n = window.innerHeight, r = Math.max(8, Math.min(e.top, n - 8)), i = Math.max(8, Math.min(e.bottom, n - 8)), a = r > n - i;
		Object.assign(this.picker.style, {
			width: `${t}px`,
			left: `${Math.max(8, Math.min(e.left, window.innerWidth - t - 8))}px`,
			top: a ? "auto" : `${i + 6}px`,
			bottom: a ? `${n - r + 6}px` : "auto",
			maxHeight: `${Math.max(0, Math.min(n - 16, (a ? r : n - i) - 14))}px`
		});
	};
	hidePicker = (e = !1) => {
		this.pickerLifetime?.abort(), this.pickerLifetime = null, this.picker?.matches(":popover-open") && this.picker.hidePopover(), this.pickerOpen = !1, e && this.opener?.focus();
	};
	pickerKey = (e) => {
		if (e.key === "Escape") {
			e.preventDefault(), e.stopPropagation(), this.hidePicker(!0), this.cancel();
			return;
		}
		if (e.key === "Tab") {
			this.hidePicker(), this.cancel();
			return;
		}
		if (![
			"ArrowDown",
			"ArrowUp",
			"Home",
			"End"
		].includes(e.key)) return;
		e.preventDefault();
		let t = [...e.currentTarget.querySelectorAll("button:not(:disabled)")], n = t.indexOf(document.activeElement);
		t[e.key === "Home" ? 0 : e.key === "End" ? t.length - 1 : (n + (e.key === "ArrowUp" ? -1 : 1) + t.length) % t.length]?.focus();
	};
	choose = (e) => {
		if (this.ready() && this.pickerOpen && this.inspected?.thinking_levels?.includes(e)) {
			if (e === this.inspected.thinking) {
				this.hidePicker(!0), this.cancel();
				return;
			}
			this.field = "thinking", this.value = e, this.commit();
		}
	};
	openAdvanced = () => {
		this.current() && !this.committing && !this.uncertain && this.dialog && this.opener && (this.hidePicker(), this.inspected && this.show(this.inspected), this.api.openDialog(this.dialog, this.opener), this.publish());
	};
	fillValues() {
		let e = this.field && this.inspected?.[rt[this.field]] || [], t = this.field ? this.inspected?.[this.field] : void 0;
		this.value = t && e.includes(t) ? t : e[0] || "";
	}
	show(e) {
		this.displayed = e, this.field = Object.keys(nt).find((t) => t !== "thinking" && e[rt[t]]?.length) || "", this.fillValues();
	}
	selectField = (e) => {
		this.current() && e in nt && (this.field = e, this.fillValues(), this.publish());
	};
	selectValue = (e) => {
		this.current() && (this.value = e, this.publish());
	};
	composition = (e) => {
		this.current() && (this.composing = e, this.publish());
	};
	open = (e) => {
		if (!this.current() || !this.ui?.readable || this.committing || this.uncertain || !this.picker) return;
		if (this.picker.matches(":popover-open")) {
			this.hidePicker(!0), this.cancel();
			return;
		}
		this.opener = e, this.pickerOpen = !0, this.positionPicker(), this.picker.showPopover(), this.picker.focus(), this.pickerLifetime?.abort(), this.pickerLifetime = new AbortController();
		let t = this.pickerLifetime.signal;
		window.addEventListener("resize", this.positionPicker, { signal: t }), document.addEventListener("scroll", this.positionPicker, {
			capture: !0,
			signal: t
		}), this.inspect();
	};
	inspect = async () => {
		if (!this.current() || !this.ui?.readable || this.read || this.committing || this.uncertain) return;
		let e = new AbortController();
		this.read = e, this.error = "", this.inspected = null, this.publish();
		try {
			let t = await this.api.inspect(e.signal);
			if (!this.current() || this.read !== e || e.signal.aborted || !(this.dialog?.open || this.picker?.matches(":popover-open"))) return;
			if (!st(t, this.api.identity) || (this.snapshot?.revision || 0) > t.revision) throw Error("Unverified scope");
			this.inspected = t, this.show(t);
		} catch {
			this.current() && !e.signal.aborted && (this.error = "Inspection failed or changed scope. Nothing was updated. Close and reopen Thinking to inspect again.");
		} finally {
			this.current() && this.read === e && (this.read = null, this.publish(), this.pickerOpen && document.activeElement === this.picker && (this.picker?.querySelector("[aria-checked=\"true\"]:not(:disabled)") || this.picker?.querySelector("button:not(:disabled)"))?.focus());
		}
	};
	cancel = () => {
		this.current() && !this.committing && (this.read?.abort(), this.read = null, this.api.reserve(!1), this.publish());
	};
	close = () => {
		this.current() && !this.committing && (this.cancel(), this.current() && this.dialog && this.api.closeDialog(this.dialog));
	};
	commit = async () => {
		if (!this.ready() || !(this.dialog?.open || this.picker?.matches(":popover-open")) || !this.field || !this.inspected) return;
		let { field: e, value: t, inspected: n } = this;
		if (!Object.hasOwn(nt, e) || !n.current_session_available || !n[rt[e]]?.includes(t) || n[e] === t) return;
		let r = {
			scope: "session",
			field: e,
			value: t,
			confirm: "session",
			expected_revision: String(n.revision)
		};
		for (let e of at) e !== "project_id" && e !== "instance_id" && (r[e] = n[e]);
		if (this.committing = !0, this.api.reserve(!0) === !1) {
			this.committing = !1, this.publish();
			return;
		}
		if (!this.current()) return;
		if (!this.ui?.safe || !(this.dialog?.open || this.picker?.matches(":popover-open")) || this.inspected !== n || !ct(n, this.snapshot)) {
			this.committing = !1, this.api.reserve(!1), this.publish();
			return;
		}
		this.publish();
		let i = !1;
		try {
			let a = await this.api.set(r);
			if (!this.current()) return;
			if (!st(a, this.api.identity) || a.revision <= n.revision || at.some((r) => a[r] !== (r === e ? t : n[r]))) throw Error("Unverified update");
			this.inspected = a, this.show(a), e === "thinking" && (i = this.pickerOpen, this.hidePicker()), this.error = "The worker confirmed the session-only update and its effective current values. No prompt was sent. Refresh before another change.";
		} catch {
			if (!this.current()) return;
			this.uncertain = !0, this.inspected = null, this.displayed = null, this.error = "The update outcome is unverified. Do not retry: the runtime preference may have changed. Close this panel and explicitly review runtime recovery. Nothing will be activated or retried automatically.";
		} finally {
			this.current() && (this.committing = !1, this.inspected = null, this.api.reserve(!1), this.publish(), i && !this.uncertain && this.opener?.focus());
		}
	};
};
function ft(e) {
	mt();
	let t = e.root.querySelector("[data-react-live-panel=\"reasoning\"]"), n = e.root.querySelector("[data-reasoning-launcher]");
	t && n && (ut = new dt(e, t, n), ut.publish());
}
function pt(e, t) {
	let n = ut;
	if (n) {
		if (it.some((t) => e[t] !== n.api.identity[t])) {
			mt();
			return;
		}
		n.snapshot = e, n.ui = t, n.inspected && e.revision > n.inspected.revision && !n.committing && (n.inspected = null, n.cancel()), n.publish();
	}
}
function mt() {
	let e = ut;
	e && (ut = null, e.read?.abort(), e.hidePicker(), e.api.reserve(!1), e.dialog && e.api.closeDialog(e.dialog), (0, u.flushSync)(() => e.root.unmount()));
}
var ht = Object.freeze({
	init: ft,
	render: pt,
	dispose: mt,
	valid: st
}), gt = {
	new: "M9 18H5l-3 3V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v5M18 14v8m-4-4h8",
	rename: "m16 3 5 5-12 12H4v-5L16 3Z M13 6l5 5",
	versions: "M3 4v5h5M3 9a9 9 0 1 1 0 6M12 7v5l3 2",
	goals: "M4 22V3m0 1h15l-3 4 3 4H4",
	processes: "M4 4h16v16H4V4Z m3 4 4 4-4 4m6 0h4",
	reasoning: "M9 18h6m-6 3h6M8 14a6 6 0 1 1 8 0c-1 1-1 2-1 4H9c0-2 0-3-1-4Z",
	compaction: "M4 3h16M4 21h16M12 5v14m-4-10 4 4 4-4m-8 6 4-4 4 4",
	steer: "M12 21V3m-5 5 5-5 5 5M5 21v-4a5 5 0 0 1 5-5h2",
	refresh: "M20 7V3m0 4h-4M4 17v4m0-4h4M20 7a9 9 0 0 0-16 1m0 9a9 9 0 0 0 16-1",
	back: "m14 6-6 6 6 6",
	next: "m10 6 6 6-6 6",
	check: "m5 12 4 4L19 6",
	chevron: "m8 10 4 4 4-4",
	shield: "M12 3 4 6v6c0 5 8 9 8 9s8-4 8-9V6l-8-3Z",
	model: "m12 3 10 5-10 5L2 8l10-5Z M2 12l10 5 10-5M2 16l10 5 10-5",
	close: "m6 6 12 12M6 18 18 6",
	power: "M12 3v9m-5-7a8 8 0 1 0 10 0",
	inspector: "M3 4h18v16H3zM15 4v16m3-11v2m0 3v2"
};
function _t({ name: e }) {
	return /* @__PURE__ */ (0, D.jsx)("svg", {
		className: e === "model" ? "icon model-trigger-icon" : "icon",
		viewBox: "0 0 24 24",
		width: e === "chevron" ? 14 : e === "close" ? 18 : 16,
		height: e === "chevron" ? 14 : e === "close" ? 18 : 16,
		fill: "none",
		stroke: "currentColor",
		strokeWidth: "1.6",
		strokeLinecap: "round",
		strokeLinejoin: "round",
		"aria-hidden": "true",
		focusable: "false",
		children: /* @__PURE__ */ (0, D.jsx)("path", { d: gt[e] })
	});
}
//#endregion
//#region src/compaction/model.ts
var vt = /* @__PURE__ */ new Set([
	"pending",
	"running",
	"completed",
	"noop",
	"fallback",
	"canceled",
	"failed",
	"uncertain"
]);
function yt(e) {
	return typeof e == "object" && !!e && !Array.isArray(e);
}
var bt = (e, t = !1) => typeof e == "string" && (t || !!e) && e.length <= 256 && !/[\u0000-\u001f\u007f]/.test(e), xt = (e) => typeof e == "number" && Number.isSafeInteger(e) && e >= 0 && e <= 1073741824, St = (e) => typeof e == "number" && Number.isSafeInteger(e) && e > 0;
function Ct(e, t) {
	return !yt(e) || typeof e.state != "string" || !vt.has(e.state) || e.session_id !== t || !bt(e.branch_id) || !bt(e.expected_tip_id, !0) || !xt(e.summarized_messages) || !xt(e.retained_messages) || typeof e.progress_done != "boolean" || typeof e.used_fallback != "boolean" ? !1 : bt(e.request_id) && bt(e.compaction_id) && e.turn_id === e.compaction_id && e.turn_origin === "compact" && St(e.root_epoch) && St(e.turn_sequence) || [
		"pending",
		"failed",
		"uncertain"
	].includes(e.state) && e.request_id === "" && e.compaction_id === "" && e.turn_id === "" && e.turn_origin === "" && e.root_epoch === 0 && e.turn_sequence === 0;
}
function wt(e, t, n) {
	if (!yt(e)) return !1;
	let r = e.compaction_ack;
	return e.project_id === t.project_id && e.instance_id === t.instance_id && e.session_id === t.session_id && St(e.revision) && e.revision >= Number(n.expected_revision) && yt(r) && bt(r.compaction_id) && r.turn_id === r.compaction_id && r.turn_origin === "compact" && St(r.root_epoch) && St(r.turn_sequence) && r.session_id === n.session_id && r.branch_id === n.branch_id;
}
function Tt(e, t) {
	let n = e?.goal;
	return !n || n.session_id !== t || !bt(n.branch_id) || !bt(n.tip_id, !0) || !St(e?.revision) ? null : {
		session_id: t,
		branch_id: n.branch_id,
		expected_tip_id: n.tip_id,
		expected_revision: String(e.revision)
	};
}
function Et(e, t, n) {
	let r = e?.goal, i = yt(e?.compaction) ? e.compaction.state : void 0;
	return !!t?.safe && !n && e?.status === "idle" && !e.cancel_requested && !e.permission && !e.input && !e.queue?.items?.length && !!r && !r.running && (!r.goal_id || ["complete", "budget_limited"].includes(r.status ?? "")) && ![
		"pending",
		"running",
		"uncertain"
	].includes(String(i));
}
function Dt(e) {
	if (!e) return "No manual compaction has been requested in this live conversation.";
	let t = `${e.summarized_messages.toLocaleString()} messages summarized · ${e.retained_messages.toLocaleString()} retained`;
	switch (e.state) {
		case "pending": return "Request reserved. Waiting for the native operation receipt. Stop cancels this operation.";
		case "running": return e.progress_done ? "Compaction progress received; waiting for native cleanup and authoritative context refresh. Stop remains available." : "Manual compaction is running. It may use provider tokens. Stop cancels this operation.";
		case "completed": return `Manual compaction completed · ${t}. Context and usage refreshed.`;
		case "noop": return "No compaction was needed. Context and usage refreshed; no message was sent.";
		case "fallback": return `Compaction used a fallback checkpoint · ${t}. Context and usage refreshed; this was not a full provider summary.`;
		case "canceled": return "Manual compaction canceled. Context and usage refreshed; provider usage or a checkpoint may already have been saved.";
		case "failed": return "Manual compaction failed or was rejected. Review the current conversation; nothing will be retried automatically.";
		case "uncertain": return "The worker disconnected before the result could be verified. Close and explicitly reopen this project to inspect saved context. Nothing will be retried.";
	}
}
//#endregion
//#region src/compaction/ManualCompaction.tsx
function Ot(e) {
	let t = e.committing || e.compaction?.state === "pending" || e.compaction?.state === "running", n = e.invalid ? "Compaction status is unavailable. Reconnect before trying again." : e.notice || (e.stopping ? "Stopping compaction…" : e.committing ? "Starting compaction…" : Dt(e.compaction));
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [(0, u.createPortal)(/* @__PURE__ */ (0, D.jsx)("button", {
		type: "button",
		className: "composer-context-button",
		"data-compaction-open": "",
		"aria-label": "Compact context",
		title: t ? "Compacting context…" : "Compact context · Uses provider tokens",
		"aria-busy": t,
		hidden: !e.supported,
		disabled: !e.canStart,
		children: /* @__PURE__ */ (0, D.jsx)(_t, { name: "compaction" })
	}), e.launcher), /* @__PURE__ */ (0, D.jsxs)("div", {
		"data-compaction-inline": "",
		className: "compaction-inline fine",
		hidden: e.dismissed || !e.compaction && !e.committing && !e.notice && !e.invalid,
		children: [/* @__PURE__ */ (0, D.jsx)("p", {
			"data-compaction-status": "",
			role: "status",
			children: n
		}), /* @__PURE__ */ (0, D.jsx)("button", {
			type: "button",
			className: "quiet icon-button",
			"data-compaction-dismiss": "",
			"aria-label": "Dismiss compaction status",
			title: "Dismiss compaction status",
			disabled: t,
			children: "×"
		})]
	})] });
}
//#endregion
//#region src/compaction/controller.tsx
var kt = null;
function At(e) {
	return kt === e && !e.committing && !!e.ui?.supported && !!e.ui.readable && Et(e.snapshot, e.ui, e.invalid) && !!Tt(e.snapshot, e.api.identity.session_id);
}
function jt(e = kt) {
	if (!e || e !== kt) return;
	let t = e.snapshot?.compaction;
	(0, u.flushSync)(() => e.root.render(/* @__PURE__ */ (0, D.jsx)(Ot, {
		launcher: e.launcher,
		compaction: Ct(t, e.api.identity.session_id) ? t : null,
		invalid: e.invalid,
		notice: e.notice,
		dismissed: e.dismissed,
		supported: !!e.ui?.supported,
		canStart: At(e),
		committing: e.committing,
		stopping: e.ui?.stopLabel === "Stopping…"
	})));
}
function Mt(e) {
	Nt();
	let t = e.root.querySelector("[data-react-live-panel=\"compaction\"]"), n = e.root.querySelector("[data-compaction-launcher]");
	if (!t || !n) return;
	let r = {
		api: e,
		root: (0, d.createRoot)(t),
		launcher: n,
		controller: new AbortController(),
		snapshot: null,
		ui: null,
		invalid: !1,
		committing: !1,
		notice: "",
		dismissed: !1
	};
	kt = r, jt(r), e.root.addEventListener("click", (e) => {
		e.target instanceof Element && (e.target.closest("button[data-compaction-open]") && Ft(r), e.target.closest("button[data-compaction-dismiss]") && (r.dismissed = !0, jt(r), r.launcher.querySelector("[data-compaction-open]")?.focus({ preventScroll: !0 })));
	}, { signal: r.controller.signal });
}
function Nt() {
	if (!kt) return;
	let e = kt;
	kt = null, e.controller.abort(), e.api.reserve(!1), (0, u.flushSync)(() => e.root.unmount());
}
function Pt(e, t) {
	kt && (e?.compaction !== kt.snapshot?.compaction && Ct(e?.compaction, kt.api.identity.session_id) && (!Ct(kt.snapshot?.compaction, kt.api.identity.session_id) || e.compaction.request_id !== kt.snapshot.compaction.request_id) && (kt.dismissed = !1), kt.snapshot = e, kt.ui = t, kt.invalid = e?.compaction != null && !Ct(e.compaction, kt.api.identity.session_id), jt());
}
async function Ft(e) {
	if (!At(e)) return;
	let t = Tt(e.snapshot, e.api.identity.session_id);
	if (!t) return;
	if (e.committing = !0, e.notice = "", e.dismissed = !1, e.api.reserve(!0) === !1) {
		e.committing = !1, jt(e);
		return;
	}
	jt(e);
	let n = !1;
	try {
		n = await e.api.commit(t);
	} catch {}
	kt === e && (e.committing = !1, e.api.reserve(!1), e.notice = n ? "" : "Could not confirm compaction. Check the current status before trying again. Nothing was retried.", jt(e));
}
var It = Object.freeze({
	init: Mt,
	render: Pt,
	dispose: Nt,
	validCompaction: Ct,
	validACK: wt
}), Lt = /* @__PURE__ */ new Set([
	"none",
	"active",
	"paused",
	"blocked",
	"usage_limited",
	"budget_limited",
	"complete"
]), Rt = (e) => typeof e == "object" && !!e && !Array.isArray(e), zt = (e, t = !1) => typeof e == "string" && (t || !!e) && e.length <= 256 && !/[\u0000-\u001f\u007f]/.test(e), Bt = (e) => typeof e == "number" && Number.isSafeInteger(e) && e >= 0;
function Vt(e, t) {
	return Rt(e) && e.session_id === t && zt(e.branch_id) && zt(e.tip_id, !0) && zt(e.goal_id, !0) && zt(e.goal_run_id, !0) && typeof e.objective == "string" && new TextEncoder().encode(e.objective).length <= 65536 && typeof e.blocked_reason == "string" && new TextEncoder().encode(e.blocked_reason).length <= 32768 && typeof e.status == "string" && Lt.has(e.status) && (e.goal_id ? e.status !== "none" : e.status === "none" && !e.running) && typeof e.deferred == "boolean" && typeof e.running == "boolean" && (!e.running || !!e.goal_run_id) && Bt(e.tokens_used) && (e.token_budget == null || Bt(e.token_budget) && e.token_budget > 0) && (e.budget_remaining == null || Bt(e.budget_remaining)) && (e.estimated_costs == null || Array.isArray(e.estimated_costs) && e.estimated_costs.length <= 32);
}
var Ht = (e) => e?.status === "complete" || e?.status === "budget_limited", Ut = (e) => ({
	provider: e.provider,
	model: e.model,
	permission: e.permission_mode,
	mode: e.mode,
	thinking: e.thinking
}), Wt = [
	"session_id",
	"branch_id",
	"tip_id",
	"goal_id",
	"goal_run_id",
	"running",
	"status",
	"deferred",
	"objective",
	"tokens_used",
	"token_budget",
	"budget_remaining"
];
function Gt(e, t, n) {
	return !!e && Number.isSafeInteger(e.revision) && e.revision > 0 && e.revision === t?.revision && !!n && Wt.every((t) => e.goal[t] === n[t]) && JSON.stringify(e.facts) === JSON.stringify(Ut(t));
}
function Kt(e) {
	return e ? /^[1-9][0-9]*$/.test(e) && Number.isSafeInteger(Number(e)) ? e : !1 : null;
}
//#endregion
//#region src/goals/GoalPanel.tsx
function qt({ view: e, actions: t }) {
	let { goal: n, ui: r, draft: i, confirmation: a, inspected: o, snapshot: s } = e, c = en(e), l = Gt(o, s, n), d = !n?.goal_id || Ht(n), f = !!n?.goal_id && !Ht(n), p = e.error || (e.invalid ? "Could not verify the goal projection. Controls are disabled." : e.read ? "Inspecting the exact saved goal; no work is being started…" : e.committing ? "Requesting one goal run…" : n?.running ? "One goal run is active across serial turns. No frontend loop or automatic restart is scheduled." : d && Kt(i.budget) === !1 ? "Enter a positive whole-token budget no larger than 9007199254740991, or leave it empty." : s?.mode === "plan" ? "Goal execution is unavailable in Plan mode. Inspection is read-only." : l ? r?.safe ? "Inspection is read-only. Nothing starts until you explicitly authorize and confirm." : "Goal execution needs an idle connected runtime without pending/review queue items or another action." : "Refresh the inspected scope before authorizing a goal run."), m = o ? `Session: ${o.goal.session_id}\nBranch: ${o.goal.branch_id}\nTip: ${o.goal.tip_id || "(empty history)"}\nGoal: ${o.goal.goal_id || "(confirmed absent)"}${l ? "" : "\nChanged: refresh inspection before continuing."}` : "Not inspected. Refresh before authorizing a goal run.", h = (e) => e == null ? "No token budget" : e.toLocaleString();
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		(0, u.createPortal)(/* @__PURE__ */ (0, D.jsxs)("button", {
			type: "button",
			className: "task-menu-trigger goal-mode-toggle",
			"data-goal-toggle": !0,
			"aria-label": "Goal",
			"aria-pressed": i.enabled,
			title: i.enabled ? "Goal mode · Submit this composer to start a goal run. Click to return to a normal message." : Qt(e) ? "Goal mode · Type an objective in the composer, then submit to start." : n?.goal_id && !Ht(n) ? "An existing goal is saved. Open Goal details to inspect or resume it." : "Goal mode requires an idle Default-mode session.",
			hidden: !r?.supported,
			disabled: e.submitting || e.committing || !i.enabled && !Qt(e),
			onClick: () => t.toggle(e),
			children: [/* @__PURE__ */ (0, D.jsx)(_t, { name: "goals" }), /* @__PURE__ */ (0, D.jsx)("span", { children: "Goal" })]
		}), e.launcher),
		/* @__PURE__ */ (0, D.jsx)("button", {
			type: "button",
			"data-goals-open": !0,
			"data-goals-available": !!r?.supported,
			hidden: !0,
			disabled: !r?.readable || e.invalid || e.committing || e.submitting,
			onClick: (n) => t.open(e, n.currentTarget),
			children: "Goal details"
		}),
		(0, u.createPortal)(/* @__PURE__ */ (0, D.jsxs)("div", {
			"data-live-goal-status": !0,
			className: "goal-inline-status",
			hidden: !n?.goal_id || Ht(n),
			children: [
				/* @__PURE__ */ (0, D.jsx)("span", {
					"data-goal-status-badge": !0,
					children: n?.running ? "Running" : n?.deferred ? "Run stopped" : n?.status.replaceAll("_", " ")
				}),
				/* @__PURE__ */ (0, D.jsx)("span", {
					"data-goal-summary-label": !0,
					className: "goal-inline-objective",
					title: n?.objective,
					children: n?.objective
				}),
				/* @__PURE__ */ (0, D.jsx)("span", {
					"data-goal-summary-usage": !0,
					children: n?.budget_remaining == null ? "" : `${n.budget_remaining.toLocaleString()} tokens left`
				}),
				/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-goal-details": !0,
					disabled: !r?.readable || e.invalid || e.committing,
					onClick: (n) => t.open(e, n.currentTarget),
					children: "Details"
				})
			]
		}), e.statusHost),
		/* @__PURE__ */ (0, D.jsxs)("dialog", {
			ref: e.dialog,
			id: "goals-dialog",
			className: "goals-dialog runtime-dialog",
			"aria-labelledby": "goals-heading",
			"aria-describedby": "goals-boundary",
			onCancel: (n) => {
				n.preventDefault(), t.close(e);
			},
			onClose: () => t.cancel(e),
			onCompositionStart: () => t.compose(e, !0),
			onCompositionEnd: () => t.compose(e, !1),
			children: [/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-heading",
				children: [
					/* @__PURE__ */ (0, D.jsx)("h2", {
						id: "goals-heading",
						children: "Thread Goal"
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "quiet",
						"data-goal-inspect": !0,
						disabled: !r?.readable || !!e.read || !!a || e.committing,
						onClick: () => void t.inspect(e),
						children: "Refresh goal"
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "quiet",
						"data-runtime-abort": !0,
						hidden: !(r?.showStop ?? r?.canStop),
						disabled: !r?.canStop,
						title: r?.stopTitle || "",
						children: r?.stopLabel || "Stop goal run"
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						ref: e.closeButton,
						type: "button",
						className: "quiet icon-button",
						"data-goals-close": !0,
						"aria-label": "Close goals",
						title: "Close",
						onClick: () => t.close(e),
						children: /* @__PURE__ */ (0, D.jsx)("svg", {
							className: "icon",
							width: "18",
							height: "18",
							viewBox: "0 0 24 24",
							fill: "none",
							stroke: "currentColor",
							strokeWidth: "1.7",
							strokeLinecap: "round",
							"aria-hidden": "true",
							children: /* @__PURE__ */ (0, D.jsx)("path", { d: "m6 6 12 12M6 18 18 6" })
						})
					})
				]
			}), /* @__PURE__ */ (0, D.jsxs)("div", {
				className: "runtime-dialog-body",
				children: [
					/* @__PURE__ */ (0, D.jsx)("p", {
						id: "goals-boundary",
						className: "fine",
						children: "Inspecting a goal is read-only. Starting or resuming is explicit: one goal run may use multiple serial turns, write files, and run commands when permitted. It uses the current model and session permissions. These are permission gates, not a sandbox."
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						"data-goal-notice": !0,
						className: "fine",
						role: "status",
						"aria-live": "polite",
						children: p
					}),
					/* @__PURE__ */ (0, D.jsxs)("dl", {
						className: "goal-facts",
						children: [
							/* @__PURE__ */ (0, D.jsx)("dt", { children: "Reviewed scope" }),
							/* @__PURE__ */ (0, D.jsx)("dd", {
								"data-goal-scope": !0,
								children: m
							}),
							/* @__PURE__ */ (0, D.jsx)("dt", { children: "Objective" }),
							/* @__PURE__ */ (0, D.jsx)("dd", {
								"data-goal-objective": !0,
								children: n?.objective || "No saved goal"
							}),
							/* @__PURE__ */ (0, D.jsx)("dt", { children: "Goal status" }),
							/* @__PURE__ */ (0, D.jsx)("dd", {
								"data-goal-state": !0,
								children: n ? `${n.status.replaceAll("_", " ")}${n.running ? " · run active" : " · no active run"}` : "Unavailable"
							}),
							/* @__PURE__ */ (0, D.jsx)("dt", { children: "Deferred" }),
							/* @__PURE__ */ (0, D.jsx)("dd", {
								"data-goal-deferred": !0,
								children: n?.deferred ? "Yes · not automatically scheduled" : "No"
							}),
							/* @__PURE__ */ (0, D.jsx)("dt", { children: "Tokens used" }),
							/* @__PURE__ */ (0, D.jsx)("dd", {
								"data-goal-tokens": !0,
								children: h(n?.tokens_used)
							}),
							/* @__PURE__ */ (0, D.jsx)("dt", { children: "Token budget" }),
							/* @__PURE__ */ (0, D.jsx)("dd", {
								"data-goal-budget": !0,
								children: h(n?.token_budget)
							}),
							/* @__PURE__ */ (0, D.jsx)("dt", { children: "Budget remaining" }),
							/* @__PURE__ */ (0, D.jsx)("dd", {
								"data-goal-remaining": !0,
								children: h(n?.budget_remaining)
							}),
							/* @__PURE__ */ (0, D.jsx)("dt", { children: "Current model / permissions" }),
							/* @__PURE__ */ (0, D.jsx)("dd", {
								"data-goal-authority": !0,
								children: s ? `${s.provider || "Unknown"} / ${s.model || "Unknown"} · ${s.permission_mode || "unknown permissions"} · ${s.mode || "unknown mode"}` : "Unknown"
							})
						]
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						"data-goal-blocked": !0,
						className: "fine",
						hidden: !n?.blocked_reason,
						children: n?.blocked_reason || ""
					}),
					/* @__PURE__ */ (0, D.jsxs)("div", {
						"data-goal-create-fields": !0,
						hidden: !d || !!a,
						children: [
							/* @__PURE__ */ (0, D.jsx)("label", {
								htmlFor: "goal-token-budget",
								children: "Optional token budget"
							}),
							/* @__PURE__ */ (0, D.jsx)("input", {
								id: "goal-token-budget",
								"data-goal-token-budget": !0,
								type: "text",
								inputMode: "numeric",
								maxLength: 16,
								autoComplete: "off",
								placeholder: "No budget",
								value: i.budget,
								onChange: (n) => t.draft(e, "budget", n.currentTarget.value)
							}),
							/* @__PURE__ */ (0, D.jsx)("p", {
								className: "fine",
								children: "Optional budget for the next new goal. Write its objective in the composer with Goal enabled; submitting starts the run using the current model and permissions. This panel does not start a new goal."
							})
						]
					}),
					/* @__PURE__ */ (0, D.jsxs)("div", {
						className: "dialog-actions",
						children: [/* @__PURE__ */ (0, D.jsx)("button", {
							ref: e.startButton,
							type: "button",
							className: "button",
							"data-goal-write": !0,
							hidden: !d || !!a,
							disabled: !Qt(e) || Kt(i.budget) === !1,
							onClick: () => t.write(e),
							children: "Write goal in composer"
						}), /* @__PURE__ */ (0, D.jsx)("button", {
							ref: e.resumeButton,
							type: "button",
							className: "button",
							"data-goal-resume": !0,
							hidden: !f || !!a,
							disabled: !c || n?.budget_remaining === 0,
							onClick: () => t.prepare(e, "goal-resume"),
							children: "Review resume…"
						})]
					}),
					/* @__PURE__ */ (0, D.jsxs)("section", {
						"data-goal-confirmation": !0,
						className: "goal-confirmation",
						"aria-labelledby": "goal-confirm-heading",
						hidden: !a,
						children: [
							/* @__PURE__ */ (0, D.jsx)("h3", {
								id: "goal-confirm-heading",
								children: "Authorize this goal run?"
							}),
							/* @__PURE__ */ (0, D.jsx)("p", {
								"data-goal-confirm-target": !0,
								children: a?.target || ""
							}),
							/* @__PURE__ */ (0, D.jsx)("p", { children: "This starts one correlated goal run, potentially across multiple serial turns. Files and commands may be changed when the current permissions allow them. Stop cancels the whole run, including gaps between turns. Your unsent message draft is kept. A finished run does not necessarily mean the goal is complete." }),
							/* @__PURE__ */ (0, D.jsxs)("label", {
								className: "goal-consent",
								children: [/* @__PURE__ */ (0, D.jsx)("input", {
									ref: e.consentInput,
									type: "checkbox",
									"data-goal-consent": !0,
									checked: e.consent,
									onChange: (n) => t.consent(e, n.currentTarget.checked)
								}), " I authorize this goal run with the model and permissions shown above."]
							}),
							/* @__PURE__ */ (0, D.jsxs)("div", {
								className: "dialog-actions",
								children: [/* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "quiet",
									"data-goal-cancel": !0,
									disabled: e.committing,
									onClick: () => t.cancel(e),
									children: "Cancel"
								}), /* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "button primary",
									"data-goal-confirm": !0,
									disabled: !c || !a || !e.consent,
									onClick: () => void t.commit(e),
									children: a?.action === "goal-resume" ? "Resume goal run" : "Start goal run"
								})]
							})
						]
					})
				]
			})]
		})
	] });
}
//#endregion
//#region src/goals/controller.tsx
var Jt = /* @__PURE__ */ new Map(), j = null;
function Yt(e) {
	Xt();
	let t = e.root?.querySelector("[data-react-live-panel=\"goals\"]"), n = e.root?.querySelector("[data-goals-launcher]"), r = e.root?.querySelector("[data-goal-composer-status]");
	if (!t || !n || !r || e.root?.dataset.goalsEnabled !== "true") return;
	let i = JSON.stringify([e.identity.project_id, e.identity.session_id]), a = Jt.get(i) || {
		enabled: !1,
		budget: ""
	};
	for (Jt.delete(i), Jt.set(i, a); Jt.size > 16;) {
		let e = Jt.keys().next();
		e.done || Jt.delete(e.value);
	}
	j = {
		api: e,
		root: (0, d.createRoot)(t),
		launcher: n,
		statusHost: r,
		draft: a,
		snapshot: null,
		goal: null,
		ui: null,
		inspected: null,
		invalid: !1,
		error: "",
		read: null,
		confirmation: null,
		committing: !1,
		submitting: !1,
		composing: !1,
		consent: !1,
		reserved: !1,
		dialog: (0, l.createRef)(),
		consentInput: (0, l.createRef)(),
		closeButton: (0, l.createRef)(),
		startButton: (0, l.createRef)(),
		resumeButton: (0, l.createRef)()
	}, tn(j);
}
function Xt() {
	if (!j) return;
	let e = j;
	j = null, e.read?.controller.abort(), e.dialog.current && e.api.closeDialog(e.dialog.current), e.reserved && (e.reserved = !1, e.api.reserve(!1)), (0, u.flushSync)(() => e.root.unmount());
}
function Zt(e, t) {
	if (!j) return;
	let n = e?.goal, r = Vt(n, j.api.identity.session_id) ? n : null;
	j.goal?.running && !r?.running && (j.inspected = null), j.snapshot = e, j.ui = t, j.invalid = e?.goal != null && !r, j.goal = r, tn(j);
}
function Qt(e) {
	return !!e.ui?.supported && !!e.ui.safe && !e.invalid && !e.read && !e.confirmation && !e.committing && !e.submitting && !!e.goal && (!e.goal.goal_id || Ht(e.goal)) && e.snapshot?.mode === "default";
}
function M() {
	return {
		enabled: !!j?.draft.enabled,
		busy: !!j?.submitting,
		available: !!j && Qt(j)
	};
}
function $t(e) {
	j !== e || e.submitting || e.committing || !e.draft.enabled && !Qt(e) || (e.draft.enabled = !e.draft.enabled, e.error = "", e.api.changed(), e.api.focusPrompt());
}
function en(e) {
	let t = e.snapshot;
	return !!e.ui?.safe && !e.invalid && !e.read && !e.committing && !e.composing && Gt(e.inspected, t, e.goal) && !e.goal?.running && t?.mode === "default" && [
		"ask",
		"allow",
		"deny"
	].includes(String(t.permission_mode)) && !!t.provider && !!t.model;
}
function tn(e) {
	j === e && (0, u.flushSync)(() => e.root.render(/* @__PURE__ */ (0, D.jsx)(qt, {
		view: e,
		actions: un
	})));
}
function nn(e) {
	e.reserved && (e.reserved = !1, e.api.reserve(!1));
}
async function rn(e, t = !1) {
	if (j !== e || !e.ui?.readable || e.invalid || e.read || e.confirmation || e.committing || !e.goal) return;
	let n = e.goal.branch_id, r = { controller: new AbortController() };
	e.read = r, e.error = "", tn(e);
	try {
		let i = await e.api.inspect(n, r.controller.signal);
		if (j !== e || e.read !== r || r.controller.signal.aborted || !(t ? e.draft.enabled : e.dialog.current?.open)) return;
		if (!Rt(i) || !Vt(i.goal, e.api.identity.session_id) || i.goal.branch_id !== n || typeof i.revision != "number" || !Number.isSafeInteger(i.revision) || i.revision <= 0) throw Error("Goal scope changed");
		e.inspected = {
			goal: i.goal,
			facts: Ut(i),
			revision: i.revision
		};
	} catch {
		j === e && e.read === r && !r.controller.signal.aborted && (e.inspected = null, e.error = "Goal inspection failed or changed scope. Nothing started. Refresh explicitly to inspect again.");
	} finally {
		j === e && e.read === r && (e.read = null, tn(e));
	}
}
function an(e) {
	if (j !== e || e.committing) return;
	let t = document.activeElement?.closest("[data-goal-confirmation]");
	e.read?.controller.abort(), e.read = null, e.confirmation = null, e.consent = !1, nn(e), tn(e), e.dialog.current?.open && t && (e.goal?.goal_id && !Ht(e.goal) ? e.resumeButton : e.startButton).current?.focus({ preventScroll: !0 });
}
function on(e) {
	j !== e || e.committing || (an(e), e.dialog.current && e.api.closeDialog(e.dialog.current));
}
function sn(e, t) {
	if (j !== e || !en(e) || e.confirmation || !e.inspected) return;
	let n = e.inspected.goal;
	if (t === "goal-resume" && n.goal_id && !Ht(n) && n.budget_remaining !== 0) {
		if (e.confirmation = {
			action: t,
			fields: {
				session_id: n.session_id,
				branch_id: n.branch_id,
				expected_tip_id: n.tip_id,
				expected_goal_id: n.goal_id
			},
			expectedRevision: e.inspected.revision,
			target: `Resume on branch ${n.branch_id}, exact tip ${n.tip_id || "(empty history)"}, reviewed goal ${n.goal_id}. Existing objective and budget remain unchanged.`
		}, e.consent = !1, !e.api.reserve(!0)) {
			e.confirmation = null;
			return;
		}
		e.reserved = !0, tn(e), e.consentInput.current?.focus({ preventScroll: !0 });
	}
}
async function cn(e) {
	if (j !== e || !en(e) || !e.confirmation || !e.consent || !e.dialog.current?.open) return;
	let t = e.confirmation;
	e.confirmation = null, e.committing = !0, e.consent = !1, tn(e), e.api.closeDialog(e.dialog.current);
	let n = !1;
	try {
		n = await e.api.run(t.action, t.fields, t.expectedRevision);
	} catch {}
	j === e && (e.committing = !1, e.inspected = null, nn(e), e.error = n ? "The goal run was admitted. Follow its current status; admission does not mean the goal is complete." : "Goal run outcome needs review. Your objective and message draft are kept. Nothing will be retried automatically.", tn(e));
}
async function ln(e, t) {
	let n = j;
	if (!n?.draft.enabled || !Qt(n)) return !1;
	let r = Kt(n.draft.budget);
	if (!n.api.validText(e) || [...e].length > 32768 || r === !1) return n.api.notice(r === !1 ? "Use a positive whole-token budget in Goal details, or leave it empty." : "A goal needs valid text within 32,768 characters and 64 KiB."), !1;
	let i = n.snapshot?.revision;
	if (!Number.isSafeInteger(i) || typeof i != "number" || !n.goal) return !1;
	let a = {
		goal: n.goal,
		facts: Ut(n.snapshot),
		revision: i
	};
	if (n.submitting = !0, n.inspected = null, !n.api.reserve(!0)) return n.submitting = !1, n.api.changed(), !1;
	n.reserved = !0;
	try {
		if (await rn(n, !0), j !== n) return !1;
		let i = n.inspected;
		if (!t() || !n.draft.enabled || !i || i.revision !== a.revision + 1 || !en(n) || !Gt({
			...a,
			revision: i.revision
		}, n.snapshot, n.goal)) return n.api.notice(n.error || "The draft or session changed before goal submission. Nothing started. Review it and submit again."), !1;
		let o = a.goal, s = {
			session_id: o.session_id,
			branch_id: o.branch_id,
			expected_tip_id: o.tip_id,
			expected_goal_id: o.goal_id,
			objective: e,
			...r ? { token_budget: r } : {}
		};
		n.committing = !0, tn(n);
		let c = await n.api.run("goal-start", s, i.revision);
		return j === n && (c && t() && (n.draft.enabled = !1), c || n.api.notice("The goal run could not be confirmed. Your draft is kept. Review the runtime before trying again; nothing will be retried automatically."), c);
	} catch {
		return j === n && n.api.notice("The goal run could not be confirmed. Your draft is kept; nothing will be retried automatically."), !1;
	} finally {
		j === n && (n.submitting = !1, n.committing = !1, n.inspected = null, nn(n), tn(n));
	}
}
var un = {
	inspect: rn,
	cancel: an,
	close: on,
	prepare: sn,
	commit: cn,
	toggle: $t,
	write(e) {
		j === e && Qt(e) && (on(e), e.draft.enabled = !0, e.api.changed(), e.api.focusPrompt());
	},
	open(e, t) {
		j === e && e.ui?.readable && !e.invalid && e.dialog.current && (e.api.openDialog(e.dialog.current, t), e.closeButton.current?.focus({ preventScroll: !0 }), rn(e));
	},
	draft(e, t, n) {
		j === e && (e.draft[t] = n, tn(e));
	},
	consent(e, t) {
		j === e && (e.consent = t, tn(e));
	},
	compose(e, t) {
		j === e && (e.composing = t, tn(e));
	}
}, dn = Object.freeze({
	init: Yt,
	render: Zt,
	dispose: Xt,
	validGoal: Vt,
	composerState: M,
	submit: ln
}), fn = /* @__PURE__ */ new Set([
	"history-branch-fork",
	"history-session-fork",
	"history-branch-rename"
]), pn = (e) => !!e && typeof e == "object", mn = (e, t = !1) => typeof e == "string" && (t || e.length > 0) && e.length <= 256 && !/[\u0000-\u001f\u007f]/.test(e), hn = (e) => mn(e) && !/[\u0080-\u009f]/.test(e), gn = (e) => typeof e == "string" && e.length <= 4096 && !/[\u0000-\u001f\u007f]/.test(e), _n = (e) => Number.isSafeInteger(e) && e >= 0;
function vn(e, t) {
	return pn(e) && e.project_id === t.project_id && e.session_id === t.session_id && e.instance_id === t.instance_id;
}
function yn(e, t) {
	if (!vn(e, t) || !_n(e.revision) || !mn(e.current_branch_id) || !mn(e.current_tip_id, !0) || !Array.isArray(e.versions) || e.versions.length > 100 || typeof e.has_more != "boolean" || !gn(e.next_cursor) || e.has_more !== !!e.next_cursor) return !1;
	let n = /* @__PURE__ */ new Set();
	return e.versions.every((t) => !pn(t) || !mn(t.branch_id) || !mn(t.tip_id, !0) || typeof t.name != "string" || t.name.length > 512 || typeof t.current != "boolean" || t.current !== (t.branch_id === e.current_branch_id) || t.current && t.tip_id !== e.current_tip_id || n.has(t.branch_id) ? !1 : (n.add(t.branch_id), !0));
}
function bn(e, t, n) {
	if (!vn(e, n) || !_n(e.revision) || e.branch_id !== t.branch_id || e.tip_id !== t.tip_id || !Array.isArray(e.messages) || e.messages.length > 256 || typeof e.has_more != "boolean" || !gn(e.next_cursor) || e.has_more !== !!e.next_cursor) return !1;
	let r = 0;
	return e.messages.every((e) => !pn(e) || typeof e.role != "string" || ![
		"user",
		"assistant",
		"tool",
		"system",
		"tool_activity"
	].includes(e.role) || typeof e.text != "string" ? !1 : (r += new TextEncoder().encode(e.text).length, r <= 1048576 && (!e.tools || Array.isArray(e.tools) && e.tools.length <= 128 && e.tools.every((e) => pn(e) && typeof e.tool == "string" && e.tool.length <= 256))));
}
function xn(e, t, n, r, i = Date.now()) {
	return vn(e, r) && e.branch_id === t.branch_id && e.tip_id === t.tip_id && e.current_branch_id === n.current_branch_id && e.current_tip_id === n.current_tip_id && mn(e.restore_token) && typeof e.expires_at == "string" && Number.isFinite(Date.parse(e.expires_at)) && Date.parse(e.expires_at) > i;
}
function Sn(e, t) {
	return typeof e == "string" && !!e && e === e.trim() && new TextEncoder().encode(e).length <= 256 && [...e].length <= (t === "history-session-fork" ? 72 : 64) && !/[\u0000-\u001f\u007f-\u009f]/.test(e);
}
function Cn(e, t) {
	return !!e && !!t && [
		"project_id",
		"instance_id",
		"session_id",
		"revision",
		"current_branch_id",
		"current_tip_id",
		"branch_id",
		"tip_id",
		"name"
	].every((n) => e[n] === t[n]);
}
function wn(e, t, n) {
	return vn(e, t) && e.branch_id === t.branch_id && e.tip_id === t.tip_id && e.name === n && _n(e.revision) && e.revision >= t.revision;
}
function Tn(e) {
	return !e || e.operation || e.phase || e.stale || !e.dialog?.open || !e.selected || !e.preview || !e.page || e.preview.revision !== e.page.revision || e.preview.revision !== e.snapshot?.revision || e.preview.branch_id !== e.selected.branch_id || e.preview.tip_id !== e.selected.tip_id ? null : Object.freeze({
		project_id: e.page.project_id,
		instance_id: e.page.instance_id,
		session_id: e.page.session_id,
		revision: e.preview.revision,
		current_branch_id: e.page.current_branch_id,
		current_tip_id: e.page.current_tip_id,
		branch_id: e.selected.branch_id,
		tip_id: e.selected.tip_id,
		name: e.selected.name
	});
}
function En(e, t) {
	if (!pn(e) || typeof e.text != "string" || e.text.length > 4096 || !Array.isArray(e.rows) || e.rows.length > 1e3) return !1;
	let n = `/?view=projects&project=${encodeURIComponent(t)}&session=`;
	return e.rows.every((e) => {
		if (!pn(e) || typeof e.name != "string" || e.name.length > 512 || typeof e.url != "string" || !e.url.startsWith(n)) return !1;
		try {
			let t = decodeURIComponent(e.url.slice(n.length));
			return !!t && t.length <= 256 && e.url === n + encodeURIComponent(t);
		} catch {
			return !1;
		}
	});
}
//#endregion
//#region src/versions/Panel.tsx
function Dn({ v: e, h: t, readable: n, restorable: r, historySafe: i, selection: a, historyChanged: o }) {
	let { ui: s, page: c, preview: l, phase: u } = e, d = c || e.retained?.page, f = l || e.retained?.preview, p = e.selected || e.retained?.selected, m = l ? e.previewIndex : e.retained?.previewIndex ?? e.previewIndex;
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("button", {
		type: "button",
		className: "quiet",
		"data-versions-open": "",
		"aria-haspopup": "dialog",
		"aria-controls": "versions-dialog",
		hidden: !s?.supported,
		disabled: !s?.readable,
		children: "Versions"
	}), /* @__PURE__ */ (0, D.jsxs)("dialog", {
		ref: (t) => {
			e.dialog = t;
		},
		id: "versions-dialog",
		className: "versions-dialog runtime-dialog",
		"aria-labelledby": "versions-heading",
		"aria-describedby": "versions-boundary",
		children: [/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "dialog-heading",
			children: [
				/* @__PURE__ */ (0, D.jsx)("h2", {
					id: "versions-heading",
					children: "Conversation versions"
				}),
				/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-versions-refresh": "",
					disabled: !n,
					children: "Refresh"
				}),
				/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-runtime-abort": "",
					hidden: !s?.showStop,
					disabled: !s?.canStop,
					title: s?.stopTitle,
					children: s?.stopLabel || "Stop"
				}),
				/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet icon-button",
					"data-versions-close": "",
					"aria-label": "Close versions",
					title: "Close",
					children: /* @__PURE__ */ (0, D.jsx)("svg", {
						className: "icon",
						width: "18",
						height: "18",
						viewBox: "0 0 24 24",
						fill: "none",
						stroke: "currentColor",
						strokeWidth: "1.7",
						strokeLinecap: "round",
						"aria-hidden": "true",
						children: /* @__PURE__ */ (0, D.jsx)("path", { d: "m6 6 12 12M6 18 18 6" })
					})
				})
			]
		}), /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "runtime-dialog-body",
			children: [
				/* @__PURE__ */ (0, D.jsx)("p", {
					id: "versions-boundary",
					className: "fine",
					children: "Preview saved conversation history without changing the active chat. Restoring changes conversation history only: it does not undo file changes or tool effects, and it never replays a prompt."
				}),
				/* @__PURE__ */ (0, D.jsx)("p", {
					"data-versions-status": "",
					className: "fine",
					role: "status",
					"aria-live": "polite",
					children: e.error || (e.operation ? "Loading a read-only version view…" : s?.readable ? e.retained ? "Previous read-only view retained. Select a version to verify it again; restore remains disabled." : s.restoreSafe ? "Read-only preview; the active chat is unchanged." : "Read-only browsing. Restore is unavailable while work, queued/review items, or another action is pending." : "This runtime is unavailable or has changed. No restore will be retried.")
				}),
				/* @__PURE__ */ (0, D.jsxs)("div", {
					className: "versions-columns",
					children: [/* @__PURE__ */ (0, D.jsxs)("section", {
						className: "versions-list-region",
						"aria-label": "Saved versions",
						children: [
							/* @__PURE__ */ (0, D.jsxs)("div", {
								"data-versions-list": "",
								className: "versions-list",
								children: [d?.versions.map((t) => /* @__PURE__ */ (0, D.jsxs)("button", {
									type: "button",
									className: "version-choice",
									"data-version-id": t.branch_id,
									"data-tip-id": t.tip_id,
									"data-version-preview": "",
									"aria-pressed": e.selected?.branch_id === t.branch_id && e.selected?.tip_id === t.tip_id,
									disabled: !n || !c,
									children: [/* @__PURE__ */ (0, D.jsxs)("span", { children: [t.name || "Unnamed version", t.current ? " · Current" : ""] }), /* @__PURE__ */ (0, D.jsx)("code", { children: `Branch: ${t.branch_id}\nTip: ${t.tip_id || "(empty history)"}` })]
								}, JSON.stringify([t.branch_id, t.tip_id]))), d && !d.versions.length && /* @__PURE__ */ (0, D.jsx)("p", {
									className: "fine",
									children: "No saved versions on this page."
								})]
							}),
							/* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "quiet",
								"data-versions-back": "",
								hidden: e.listIndex <= 0,
								disabled: !n || !c,
								children: "Previous versions page"
							}),
							/* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "quiet",
								"data-versions-more": "",
								hidden: !d?.has_more,
								disabled: !n || !c,
								children: "Next versions page"
							})
						]
					}), /* @__PURE__ */ (0, D.jsxs)("section", {
						className: "versions-preview",
						"aria-labelledby": "version-preview-heading",
						children: [
							/* @__PURE__ */ (0, D.jsx)("h3", {
								id: "version-preview-heading",
								children: "Read-only preview"
							}),
							/* @__PURE__ */ (0, D.jsx)("p", {
								"data-version-preview-title": "",
								className: "fine",
								children: p ? `${p.name || "Unnamed version"} · read-only preview page ${m + 1}` : "Choose a saved version."
							}),
							/* @__PURE__ */ (0, D.jsx)("code", {
								"data-version-tip": "",
								className: "version-tip",
								children: p ? `Branch: ${p.branch_id}\nTip: ${p.tip_id || "(empty history)"}` : ""
							}),
							/* @__PURE__ */ (0, D.jsx)("p", {
								"data-version-preview-notice": "",
								className: "fine",
								hidden: !f,
								children: f ? `Only this bounded preview page is shown. Restore selects the complete saved version.${f.history_truncated ? " Some saved history is omitted from this preview." : ""}${f.history_tools_truncated ? " Some tool details are omitted." : ""}` : ""
							}),
							/* @__PURE__ */ (0, D.jsx)("div", {
								"data-version-preview-messages": "",
								className: "versions-preview-messages",
								"aria-label": "Read-only saved conversation",
								children: f?.messages.map((e, t) => /* @__PURE__ */ (0, D.jsxs)("article", {
									className: "version-preview-message",
									children: [
										/* @__PURE__ */ (0, D.jsx)("strong", { children: e.role }),
										/* @__PURE__ */ (0, D.jsx)("pre", { children: e.text }),
										(e.tools || []).map((e, t) => /* @__PURE__ */ (0, D.jsx)("pre", { children: `Tool: ${typeof e.tool == "string" ? e.tool.slice(0, 256) : "saved tool"} · ${typeof e.status == "string" ? e.status.slice(0, 64) : "saved"}` }, t))
									]
								}, t))
							}),
							/* @__PURE__ */ (0, D.jsxs)("div", {
								className: "versions-preview-pagination",
								children: [/* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "quiet",
									"data-version-preview-back": "",
									hidden: e.previewIndex <= 0,
									disabled: !n || !l,
									children: "Previous preview page"
								}), /* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "quiet",
									"data-version-preview-more": "",
									hidden: !f?.has_more,
									disabled: !n || !l,
									children: "Next preview page"
								})]
							}),
							/* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "button",
								"data-version-restore": "",
								hidden: !f || !!p?.current || !!u,
								disabled: !r,
								title: s?.restoreSafe ? "Prepare an explicit conversation-history-only restore" : "Restore requires an idle, connected conversation with no pending/review queue or conflicting action",
								children: "Restore this version…"
							}),
							/* @__PURE__ */ (0, D.jsxs)("section", {
								"data-version-restore-confirmation": "",
								className: "version-restore-confirmation",
								"aria-labelledby": "version-restore-heading",
								hidden: !u,
								children: [
									/* @__PURE__ */ (0, D.jsx)("h3", {
										id: "version-restore-heading",
										children: "Restore this conversation version?"
									}),
									/* @__PURE__ */ (0, D.jsx)("p", { children: "Only the active conversation history changes. Earlier file changes and tool effects are not undone. No prompt is replayed and no new answer is generated. Your unsent draft is kept." }),
									/* @__PURE__ */ (0, D.jsx)("p", {
										"data-version-restore-target": "",
										className: "fine",
										children: e.restoreTarget
									}),
									/* @__PURE__ */ (0, D.jsx)("p", {
										"data-version-restore-status": "",
										className: "fine",
										role: "status",
										children: u === "preparing" ? "Preparing confirmation only. Nothing has changed." : u === "ready" ? "Confirm to select this saved history without starting a turn." : u === "committing" ? "Restoring conversation history…" : ""
									}),
									/* @__PURE__ */ (0, D.jsxs)("div", {
										className: "dialog-actions",
										children: [/* @__PURE__ */ (0, D.jsx)("button", {
											type: "button",
											className: "quiet",
											"data-version-restore-cancel": "",
											disabled: u === "committing",
											children: "Cancel restore"
										}), /* @__PURE__ */ (0, D.jsx)("button", {
											type: "button",
											className: "button danger",
											"data-version-restore-confirm": "",
											disabled: u !== "ready" || !r || !e.token || Date.now() >= (e.expires ?? 0),
											children: "Restore conversation version"
										})]
									})
								]
							})
						]
					})]
				}),
				e.api.root.dataset.historyControlEnabled === "true" && /* @__PURE__ */ (0, D.jsx)(On, {
					h: t,
					selected: a,
					safe: i,
					changed: o
				})
			]
		})]
	})] });
}
function On({ h: e, selected: t, safe: n, changed: r }) {
	let i = n && !!t, a = e?.confirmation;
	return /* @__PURE__ */ (0, D.jsxs)("section", {
		"data-history-controls": "",
		className: "history-controls",
		"aria-labelledby": "history-controls-heading",
		hidden: e?.ui?.supported !== !0,
		children: [
			/* @__PURE__ */ (0, D.jsx)("h3", {
				id: "history-controls-heading",
				children: "Use this saved history"
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				children: "Fork a branch here, create a detached conversation, or rename the selected branch. These are conversation-history actions, not filesystem undo: no workspace files or worktrees are created or restored, and no prompts or tools are replayed."
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				"data-history-notice": "",
				className: "fine",
				role: "status",
				"aria-live": "polite",
				children: e?.error || (e?.busy ? "Applying history change…" : t ? e?.ui?.safe ? "These actions change saved conversation history only. Your unsent message draft is kept." : "History changes require an idle, connected conversation with no pending goal, unfinished turn, queued/review work or conflicting action." : "Preview a saved version at the current revision before choosing an action. Refresh Versions if this selection has changed.")
			}),
			/* @__PURE__ */ (0, D.jsxs)("form", {
				"data-history-form": "",
				autoComplete: "off",
				children: [
					/* @__PURE__ */ (0, D.jsx)("label", {
						htmlFor: "history-control-name",
						children: "New branch or conversation name"
					}),
					/* @__PURE__ */ (0, D.jsx)("input", {
						id: "history-control-name",
						"data-history-name": "",
						name: "history_name_draft",
						type: "text",
						maxLength: 256,
						autoComplete: "off",
						enterKeyHint: "done",
						placeholder: "Name for the selected action",
						required: !0,
						value: e?.draft.name || "",
						onChange: (t) => {
							e && (e.draft.name = t.currentTarget.value, t.currentTarget.setCustomValidity(""), r());
						},
						disabled: !!a || !!e?.busy
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						className: "fine",
						children: "Branch names: up to 64 characters. Conversation names: up to 72. Both allow up to 256 UTF-8 bytes; no surrounding spaces or control characters."
					}),
					/* @__PURE__ */ (0, D.jsxs)("div", {
						className: "dialog-actions history-control-actions",
						children: [
							/* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "button",
								"data-history-review": "history-branch-fork",
								disabled: !i || !!a,
								children: "Fork branch here…"
							}),
							/* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "button",
								"data-history-action": "history-session-fork",
								disabled: !i || !!a,
								children: "Create detached conversation"
							}),
							/* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "quiet",
								"data-history-action": "history-branch-rename",
								disabled: !i || !!a,
								children: "Rename selected branch"
							})
						]
					}),
					/* @__PURE__ */ (0, D.jsxs)("section", {
						"data-history-confirmation": "",
						className: "history-control-confirmation",
						"aria-labelledby": "history-confirm-heading",
						hidden: !a,
						children: [
							/* @__PURE__ */ (0, D.jsx)("h4", {
								id: "history-confirm-heading",
								children: "Confirm this history action"
							}),
							/* @__PURE__ */ (0, D.jsx)("p", {
								"data-history-target": "",
								children: e?.targetText
							}),
							/* @__PURE__ */ (0, D.jsx)("p", { children: "No workspace files are undone, no tools are replayed, and no message is sent. Forking a branch activates it; forking a detached conversation does not open it. Your unsent composer draft is kept." }),
							/* @__PURE__ */ (0, D.jsxs)("label", {
								className: "history-control-consent",
								children: [/* @__PURE__ */ (0, D.jsx)("input", {
									type: "checkbox",
									"data-history-consent": "",
									checked: !!e?.consent,
									onChange: (t) => {
										e && (e.consent = t.currentTarget.checked, r());
									}
								}), " I confirm this action on the exact saved branch and tip shown above."]
							}),
							/* @__PURE__ */ (0, D.jsxs)("div", {
								className: "dialog-actions",
								children: [/* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "quiet",
									"data-history-cancel": "",
									disabled: !!e?.busy,
									children: "Cancel"
								}), /* @__PURE__ */ (0, D.jsx)("button", {
									type: "submit",
									className: "button primary",
									"data-history-confirm": "",
									disabled: !i || !a || !Cn(a.target, t) || !e?.consent,
									children: e?.confirmLabel || "Confirm history action"
								})]
							})
						]
					})
				]
			}),
			e?.inventory && /* @__PURE__ */ (0, D.jsxs)("div", {
				className: "fine",
				"data-history-inventory": "",
				children: [/* @__PURE__ */ (0, D.jsx)("p", { children: e.inventory.text }), !!e.inventory.rows.length && /* @__PURE__ */ (0, D.jsx)("ul", { children: e.inventory.rows.map((e, t) => /* @__PURE__ */ (0, D.jsx)("li", { children: /* @__PURE__ */ (0, D.jsxs)("a", {
					href: e.url,
					"data-snow-navigation": "",
					children: ["Open saved conversation: ", e.name || "Untitled"]
				}) }, `${e.url}:${t}`)) })]
			})
		]
	});
}
//#endregion
//#region src/versions/controller.tsx
var N = null, P = null, kn = /* @__PURE__ */ new Map(), An = (e, t) => t.querySelector(e);
function F() {
	let e = N;
	e && (0, u.flushSync)(() => e.root.render(/* @__PURE__ */ (0, D.jsx)(Dn, {
		v: e,
		h: P,
		readable: Nn(),
		restorable: Pn(),
		historySafe: Zn(),
		selection: Yn(),
		historyChanged: F
	})));
}
function jn(e) {
	Mn();
	let t = An("[data-react-live-panel=\"versions\"]", e.root);
	if (!t || e.root.dataset.versionsEnabled !== "true") return;
	let n = new AbortController(), r = {
		api: {
			...e,
			identity: Object.freeze({ ...e.identity })
		},
		host: t,
		root: (0, d.createRoot)(t),
		dialog: null,
		controller: n,
		page: null,
		selected: null,
		preview: null,
		listCursors: [""],
		previewCursors: [""],
		listIndex: 0,
		previewIndex: 0
	};
	N = r, F();
	let i = r.dialog;
	if (!i) {
		Mn();
		return;
	}
	e.root.addEventListener("click", Kn, { signal: n.signal }), i.addEventListener("cancel", (e) => {
		e.preventDefault(), zn();
	}, { signal: n.signal }), i.addEventListener("close", () => {
		N === r && r.phase !== "committing" && Rn();
	}, { signal: n.signal });
}
function Mn() {
	if (!N) return;
	let e = N;
	N = null, e.operation?.controller.abort(), e.controller.abort(), e.token = "", P?.api.root === e.api.root && Jn(), e.dialog && e.api.closeDialog(e.dialog), (0, u.flushSync)(() => e.root.unmount());
}
function Nn() {
	return !!N?.ui?.readable && !N.operation && !N.phase;
}
function Pn() {
	return !!N?.ui?.restoreSafe && !N.stale && !!N.selected && !!N.preview && !N.selected.current && !N.operation && N.preview.branch_id === N.selected.branch_id && N.preview.tip_id === N.selected.tip_id;
}
function Fn(e, t) {
	N && (N.ui = t, N.snapshot = e, F());
}
function In() {
	return Tn(N);
}
function Ln() {
	return Hn(0, !0);
}
function Rn() {
	N && N.phase !== "committing" && (N.operation?.controller.abort(), N.operation = null, N.phase = null, N.token = "", N.api.restoreState(null), F(), N?.dialog?.open && An("[data-version-restore]", N.dialog)?.focus({ preventScroll: !0 }));
}
function zn() {
	N?.phase !== "committing" && (Rn(), N?.dialog && N.api.closeDialog(N.dialog));
}
function Bn(e, t) {
	let n = {
		kind: t,
		controller: new AbortController()
	};
	return e.operation?.controller.abort(), e.operation = n, e.error = "", F(), n;
}
function Vn(e, t) {
	return N === e && e.operation === t && !t.controller.signal.aborted && e.dialog?.open;
}
async function Hn(e = 0, t = !1) {
	if (!N || !Nn() || !N.dialog?.open || e < 0 || e >= 64) return;
	t && (N.page && (N.retained = {
		page: N.page,
		selected: N.selected || N.retained?.selected,
		preview: N.preview || N.retained?.preview,
		previewIndex: N.previewIndex
	}), N.stale = !0, N.listCursors = [""], N.listIndex = 0, N.page = null, N.preview = null, N.selected = null);
	let n = N.listCursors[e];
	if (!gn(n)) return;
	let r = N, i = Bn(r, "list");
	try {
		let a = await r.api.list(n, i.controller.signal);
		if (!Vn(r, i)) return;
		if (!yn(a, r.api.identity) || e > 0 && (a.current_branch_id !== r.page?.current_branch_id || a.current_tip_id !== r.page?.current_tip_id) || a.has_more && r.listCursors.slice(0, e + 1).includes(a.next_cursor)) throw Error("Unverified versions page");
		r.page = a, t && (r.stale = !1), r.listIndex = e, r.listCursors = r.listCursors.slice(0, e + 1), a.has_more && r.listCursors.push(a.next_cursor);
	} catch {
		Vn(r, i) && (r.stale = !0, r.error = "Could not verify this versions page. Nothing changed. Refresh explicitly to read again.");
	} finally {
		N === r && r.operation === i && (r.operation = null, F());
	}
}
async function Un(e, t = 0) {
	if (!N || !Nn() || !e || t < 0 || t >= 64) return;
	e !== N.selected && ((e.branch_id !== N.retained?.selected?.branch_id || e.tip_id !== N.retained?.selected?.tip_id) && (N.retained = null), N.selected = e, N.preview = null, N.previewCursors = [""], N.previewIndex = 0);
	let n = N.previewCursors[t];
	if (!gn(n)) return;
	let r = N, i = Bn(r, "preview");
	try {
		let a = await r.api.preview(e, n, i.controller.signal);
		if (!Vn(r, i)) return;
		if (!bn(a, e, r.api.identity) || a.has_more && r.previewCursors.slice(0, t + 1).includes(a.next_cursor)) throw Error("Unverified preview");
		r.preview = a, r.retained = null, r.previewIndex = t, r.previewCursors = r.previewCursors.slice(0, t + 1), a.has_more && r.previewCursors.push(a.next_cursor);
	} catch {
		Vn(r, i) && (r.stale = !0, r.error = "Could not verify the selected branch and tip. The active chat is unchanged; refresh explicitly to read again.");
	} finally {
		N === r && r.operation === i && (r.operation = null, F());
	}
}
async function Wn() {
	if (!N || !Pn() || N.phase || !N.dialog?.open || !N.selected || !N.page) return;
	let e = N.selected, t = N.page, n = N, r = Bn(n, "prepare");
	n.phase = "preparing", n.restoreTarget = `Selected branch: ${e.branch_id}; exact tip: ${e.tip_id || "(empty history)"}.`, n.api.restoreState("preparing"), F(), n.dialog && An("[data-version-restore-cancel]", n.dialog)?.focus({ preventScroll: !0 });
	try {
		let i = await n.api.prepare(e, t, r.controller.signal);
		if (!Vn(n, r)) return;
		if (!xn(i, e, t, n.api.identity)) throw Error("Unverified restore preparation");
		n.token = i.restore_token, n.expires = Date.parse(i.expires_at), n.phase = "ready", n.api.restoreState("ready");
	} catch {
		Vn(n, r) && (n.token = "", n.phase = null, n.stale = !0, n.error = "Restore preparation was rejected or could not be verified. Nothing changed. Refresh before explicitly preparing again.", n.api.restoreState(null));
	} finally {
		N === n && n.operation === r && (n.operation = null, F());
	}
}
async function Gn() {
	if (!N || !Pn() || N.phase !== "ready" || !N.token || Date.now() >= (N.expires ?? 0) || !N.dialog?.open) return;
	let e = N, t = N.token, n = N.dialog;
	e.token = "", e.phase = "committing", e.api.restoreState("committing"), e.api.closeDialog(n);
	let r = await e.api.commit(t);
	N === e && (e.phase = null, e.api.restoreState(null), e.error = r ? "Saved conversation version restored. No prompt was replayed." : "Restore outcome needs review. Your draft is kept; nothing will be retried automatically. Review the workspace before continuing.", F());
}
function Kn(e) {
	let t = e.target instanceof Element ? e.target.closest("button") : null;
	N && t && (t.matches("[data-versions-open]") && N.ui?.readable && N.dialog ? (N.api.openDialog(N.dialog, t), Hn(0, !0)) : t.matches("[data-versions-close]") ? zn() : t.matches("[data-versions-refresh]") ? Hn(0, !0) : t.matches("[data-versions-more]") ? Hn(N.listIndex + 1) : t.matches("[data-versions-back]") ? Hn(N.listIndex - 1) : t.matches("[data-version-preview]") ? Un(N.page?.versions.find((e) => e.branch_id === t.dataset.versionId && e.tip_id === t.dataset.tipId)) : t.matches("[data-version-preview-more]") ? Un(N.selected, N.previewIndex + 1) : t.matches("[data-version-preview-back]") ? Un(N.selected, N.previewIndex - 1) : t.matches("[data-version-restore]") ? Wn() : t.matches("[data-version-restore-cancel]") ? Rn() : t.matches("[data-version-restore-confirm]") && Gn());
}
function qn(e) {
	Jn();
	let t = An("[data-history-controls]", e.root), n = An("#versions-dialog", e.root);
	if (!N || N.api.root !== e.root || !t || !n || e.root.dataset.historyControlEnabled !== "true") return;
	let r = JSON.stringify([e.identity.project_id, e.identity.session_id]), i = kn.get(r) || { name: "" };
	for (kn.delete(r), kn.set(r, i); kn.size > 16;) kn.delete(kn.keys().next().value);
	let a = new AbortController(), o = {
		api: {
			...e,
			identity: Object.freeze({ ...e.identity })
		},
		panel: t,
		dialog: n,
		draft: i,
		controller: a,
		consent: !1
	};
	P = o, t.addEventListener("click", rr, { signal: a.signal }), t.addEventListener("submit", (e) => {
		e.preventDefault(), tr();
	}, { signal: a.signal }), t.addEventListener("input", (e) => {
		P === o && (e.target instanceof HTMLInputElement && e.target.matches("[data-history-name]") && (i.name = e.target.value, e.target.setCustomValidity("")), e.target instanceof HTMLInputElement && e.target.matches("[data-history-consent]") && (o.consent = e.target.checked), F());
	}, { signal: a.signal }), t.addEventListener("change", (e) => {
		P === o && (e.target instanceof HTMLInputElement && e.target.matches("[data-history-consent]") && (o.consent = e.target.checked), F());
	}, { signal: a.signal });
	for (let e of ["compositionstart", "compositionend"]) t.addEventListener(e, () => {
		P === o && (o.composing = e === "compositionstart", F());
	}, { signal: a.signal });
	n.addEventListener("close", () => {
		P === o && !o.busy && er();
	}, { signal: a.signal }), F();
}
function Jn() {
	if (!P) return;
	let e = P;
	P = null, e.controller.abort(), F();
}
function Yn() {
	let e = In(), t = P?.api.identity;
	return e && t && [
		"project_id",
		"instance_id",
		"session_id"
	].every((n) => e[n] === t[n]) && Number.isSafeInteger(e.revision) && e.revision > 0 && e.revision === P?.snapshot?.revision ? e : null;
}
function Xn(e) {
	let t = P;
	t && N && t.api.root === N.api.root && t.dialog === N.dialog && N.host.isConnected && t.panel.isConnected && En(e, t.api.identity.project_id) && (t.inventory = {
		text: e.text,
		rows: e.rows.map(({ name: e, url: t }) => ({
			name: e,
			url: t
		}))
	}, F());
}
function Zn() {
	return !!P?.ui?.safe && !P.busy && !P.invalid && !P.composing;
}
function Qn(e, t) {
	P && (P.snapshot = e, P.ui = t, F());
}
function $n(e) {
	if (!P || !fn.has(e) || !Zn() || P.confirmation || !P.dialog.open) return;
	let t = Yn();
	if (!t) return;
	let n = An("[data-history-name]", P.panel);
	if (!n) return;
	let r = n.value;
	if (!Sn(r, e)) {
		n.setCustomValidity(`Use a nonempty name with no surrounding spaces or control characters, at most ${e === "history-session-fork" ? 72 : 64} characters and 256 UTF-8 bytes.`), n.reportValidity();
		return;
	}
	if (e === "history-branch-rename" && !Sn(t.name, e)) {
		P.error = "The saved label cannot authorize this rename. Refresh Versions.", F();
		return;
	}
	if (P.error = "", e !== "history-branch-fork") {
		nr(e, t, r);
		return;
	}
	if (P.confirmation = {
		action: e,
		target: Object.freeze({ ...t }),
		name: r
	}, P.consent = !1, P.targetText = `Selected branch ${t.branch_id}; exact saved tip ${t.tip_id || "(empty history)"}. Name: ${r}. Create and activate a new branch in this conversation. It inherits the selected history's mode and effective thinking; current provider, model and permissions stay unchanged.`, P.confirmLabel = "Create and activate branch", P.api.reserve(!0) === !1) {
		P.confirmation = null, F();
		return;
	}
	F(), P && An("[data-history-consent]", P.panel)?.focus({ preventScroll: !0 });
}
function er() {
	P && !P.busy && (P.confirmation = null, P.consent = !1, P.api.reserve(!1), F());
}
async function tr() {
	if (!P || !Zn() || !P.confirmation || !P.dialog.open || !P.consent || !Cn(P.confirmation.target, Yn())) return;
	let { action: e, target: t, name: n } = P.confirmation;
	await nr(e, t, n);
}
async function nr(e, t, n) {
	if (!P || !Zn() || !P.dialog.open || !Cn(t, Yn())) return;
	let r = P;
	if (r.busy = !0, r.api.reserve(!0) === !1) {
		r.busy = !1, F();
		return;
	}
	r.confirmation = null, r.consent = !1;
	let i = {
		session_id: t.session_id,
		expected_revision: String(t.revision),
		current_branch_id: t.current_branch_id,
		current_tip_id: t.current_tip_id,
		branch_id: t.branch_id,
		tip_id: t.tip_id,
		name: n
	};
	e === "history-branch-rename" && (i.old_name = t.name), F();
	let a;
	try {
		a = await r.api.mutate(e, i);
	} catch {
		a = null;
	}
	if (P === r) {
		if (r.busy = !1, r.api.reserve(!1), r.invalid = !0, !a) {
			r.error = "History action outcome needs review. Your drafts are kept. Nothing will be retried automatically; reload the workspace before continuing.", F();
			return;
		}
		if (e !== "history-branch-fork" && (!wn(a, t, n) || e === "history-session-fork" && (!hn(a.child_session_id) || a.child_session_id === t.session_id))) {
			r.error = "Unverified history response. Reload the workspace; do not retry this mutation.", F();
			return;
		}
		r.invalid = !1, r.error = e === "history-session-fork" ? "Detached conversation created. Refreshing saved-conversation inventory; choose Open explicitly to use it. Current history is unchanged." : e === "history-branch-rename" ? "Selected branch renamed. Refresh Versions before reviewing another action." : "New branch activated. No prompt or tools were replayed.", e === "history-session-fork" && pn(a) && typeof a.child_session_id == "string" && r.api.refreshInventory?.(a.child_session_id), e === "history-branch-rename" && Ln(), F();
	}
}
function rr(e) {
	let t = e.target instanceof Element ? e.target.closest("button") : null;
	P && t && (t.matches("[data-history-review], [data-history-action]") ? $n(t.dataset.historyAction || t.dataset.historyReview) : t.matches("[data-history-cancel]") && er());
}
var ir = Object.freeze({
	init: jn,
	render: Fn,
	dispose: Mn,
	selection: In,
	refresh: Ln
}), ar = Object.freeze({
	init: qn,
	render: Qn,
	dispose: Jn,
	selectionChanged: F,
	validName: Sn,
	renderInventory: Xn
}), or = {
	ask: "Ask",
	deny: "Deny",
	allow: "Allow"
}, sr = (e) => typeof e == "string" && Object.hasOwn(or, e), cr = (e) => [
	"running",
	"permission",
	"input"
].includes(e || ""), lr = (e) => !!e.trim() && new TextEncoder().encode(e.trim()).length <= 256, ur = (e) => !!e && typeof e == "object" && !Array.isArray(e), dr = (e) => typeof e == "string" && !!e;
function fr(e) {
	return Array.isArray(e) ? e.filter(ur).filter((e) => dr(e.session_id)).map((e) => ({
		session_id: String(e.session_id),
		name: typeof e.name == "string" ? e.name : ""
	})) : null;
}
function pr(e, t, n) {
	if (!ur(e) || e.instance_id !== t || e.project_id !== void 0 && e.project_id !== n || !Array.isArray(e.models)) return null;
	let r = fr(e.sessions);
	return r ? {
		instance_id: t,
		project_id: n,
		sessions: r,
		models: e.models.filter(ur).filter((e) => dr(e.provider) && dr(e.id)).map((e) => ({
			provider: String(e.provider),
			id: String(e.id),
			name: typeof e.name == "string" ? e.name : "",
			context_window: typeof e.context_window == "number" ? e.context_window : void 0
		})),
		sessions_available: e.sessions_available !== !1,
		sessions_truncated: e.sessions_truncated === !0,
		models_partial: e.models_partial === !0,
		models_truncated: e.models_truncated === !0
	} : null;
}
var mr = (e) => typeof e == "number" && Number.isFinite(e) && e >= 0 ? e.toLocaleString() : "Unknown";
function hr(e) {
	let t = e?.cost;
	if (!e?.available || t?.known !== !0 || !/^[A-Z]{3}$/.test(t.currency || "") || typeof t.total != "number" || !Number.isFinite(t.total) || t.total < 0) return {
		known: !1,
		value: "Unknown · cost not available"
	};
	let n = t.total, r = n === 0 ? "0.00" : n < 1e-6 || n >= 1e9 ? n.toExponential(3) : n.toLocaleString(void 0, {
		minimumFractionDigits: 2,
		maximumFractionDigits: 6
	});
	return {
		known: !0,
		value: `${t.currency} ${r}`
	};
}
function gr(e) {
	let t = e.telemetry, n = hr(t);
	return {
		contextLabel: t?.context_available && t.estimated === !1 ? "Last reported input" : "Estimated context",
		context: t?.context_available ? `${mr(t.context_tokens)} tokens / ${(t.context_window || 0) > 0 ? mr(t.context_window) : "unknown window"}` : "Unknown",
		usage: t?.available ? `${mr(t.input_tokens)} in · ${mr(t.output_tokens)} out · ${mr(t.total_tokens)} total` : "Unknown",
		cost: n.known ? n.value : "Unknown",
		knownCost: n.known
	};
}
function _r(e) {
	let t = !!e?.context_available && typeof e.context_tokens == "number" && Number.isFinite(e.context_tokens) && e.context_tokens >= 0 && typeof e.context_window == "number" && Number.isFinite(e.context_window) && e.context_window > 0;
	return {
		known: t,
		percent: t ? Math.max(0, Math.min(100, e.context_tokens / e.context_window * 100)) : 0
	};
}
function vr(e) {
	return e.goal?.running ? "Goal running" : e.status === "idle" && e.cancel_requested ? "Stopping" : {
		idle: "Ready",
		running: "Working",
		permission: "Approval needed",
		input: "Input needed",
		opening: "Opening",
		closing: "Closing",
		failed: "Worker failed"
	}[e.status] || "Unavailable";
}
//#endregion
//#region src/conversation/Surfaces.tsx
function yr({ c: e, kind: t, children: n, className: r = "", label: i, title: a = i, disabled: o = !1 }) {
	let s = (0, l.useRef)(null), c = e.menu?.trigger === s.current ? e.menu : null;
	return /* @__PURE__ */ (0, D.jsx)("button", {
		ref: s,
		type: "button",
		className: `task-menu-trigger ${r}`,
		[`data-${t}-menu`]: "",
		"aria-haspopup": t === "model" || t === "telemetry" ? "dialog" : "menu",
		"aria-expanded": !!c,
		"aria-controls": c?.panel.id,
		"aria-label": i,
		title: a,
		disabled: o,
		onClick: (n) => e.showMenu(t, n.currentTarget),
		children: n
	});
}
function br({ c: e }) {
	return /* @__PURE__ */ (0, D.jsxs)("header", {
		className: "workspace-heading live-header",
		children: [/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "conversation-title",
			children: [/* @__PURE__ */ (0, D.jsx)("strong", {
				"data-live-title": "",
				children: e.snapshot.session_name || "New conversation"
			}), /* @__PURE__ */ (0, D.jsx)("span", {
				className: "pill",
				id: "live-status",
				role: "status",
				children: vr(e.snapshot)
			})]
		}), /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "live-controls",
			children: [
				e.workflow && /* @__PURE__ */ (0, D.jsx)(yr, {
					c: e,
					kind: "session",
					className: "quiet session-menu-trigger",
					label: "Conversation actions",
					children: /* @__PURE__ */ (0, D.jsxs)("svg", {
						className: "icon",
						viewBox: "0 0 24 24",
						fill: "currentColor",
						"aria-hidden": "true",
						children: [
							/* @__PURE__ */ (0, D.jsx)("circle", {
								cx: "5",
								cy: "12",
								r: "1.5"
							}),
							/* @__PURE__ */ (0, D.jsx)("circle", {
								cx: "12",
								cy: "12",
								r: "1.5"
							}),
							/* @__PURE__ */ (0, D.jsx)("circle", {
								cx: "19",
								cy: "12",
								r: "1.5"
							})
						]
					})
				}),
				/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet icon-button",
					"data-runtime-close": "",
					"aria-label": "Close runtime",
					title: "Close runtime",
					disabled: e.controls.closeDisabled ?? !0,
					children: /* @__PURE__ */ (0, D.jsx)(_t, { name: "power" })
				}),
				/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet icon-button",
					"data-inspector-toggle": "",
					"aria-controls": "project-inspector",
					"aria-expanded": !!e.controls.inspectorExpanded,
					"aria-label": "Files and changes",
					title: "Files and changes",
					children: /* @__PURE__ */ (0, D.jsx)(_t, { name: "inspector" })
				})
			]
		})]
	});
}
function xr({ c: e }) {
	let t = e.snapshot.mode, n = t === "plan" ? "Plan Mode" : t === "default" ? "Default" : "Mode unknown", r = e.controls.verified && [
		"idle",
		"running",
		"permission",
		"input"
	].includes(e.snapshot.status) && sr(e.snapshot.permission_mode) ? or[e.snapshot.permission_mode] : "Unknown";
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [e.policy && /* @__PURE__ */ (0, D.jsxs)(yr, {
		c: e,
		kind: "permission-policy",
		className: "permission-policy-trigger",
		label: `Session permissions: ${r}`,
		title: `Session permissions: ${r} · not a sandbox`,
		disabled: !e.canSetPolicy(),
		children: [
			/* @__PURE__ */ (0, D.jsx)(_t, { name: "shield" }),
			/* @__PURE__ */ (0, D.jsx)("span", {
				"data-permission-policy-label": "",
				children: r
			}),
			/* @__PURE__ */ (0, D.jsx)(_t, { name: "chevron" })
		]
	}), e.workflow ? /* @__PURE__ */ (0, D.jsxs)(yr, {
		c: e,
		kind: "mode",
		label: `Collaboration mode: ${n}`,
		children: [
			/* @__PURE__ */ (0, D.jsx)("span", {
				"data-mode-label": "",
				children: n
			}),
			/* @__PURE__ */ (0, D.jsx)("span", {
				className: "mode-short",
				"data-mode-short": "",
				"aria-hidden": "true",
				children: t === "plan" ? "Plan" : t === "default" ? "Default" : "Mode"
			}),
			/* @__PURE__ */ (0, D.jsx)(_t, { name: "chevron" })
		]
	}) : /* @__PURE__ */ (0, D.jsx)("span", {
		className: "workspace-chip",
		title: e.projectPath,
		children: e.projectName
	})] });
}
function Sr({ c: e }) {
	let t = (0, l.useRef)(null), n = e.menu?.trigger === t.current ? e.menu : null, { known: r, percent: i } = _r(e.snapshot.telemetry);
	return e.workflow ? /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)(yr, {
		c: e,
		kind: "model",
		className: "model-menu-trigger",
		label: `Choose model: ${e.modelName()}`,
		title: `${e.snapshot.provider || "Unknown provider"} / ${e.snapshot.model || "Unknown model"}`,
		children: [
			/* @__PURE__ */ (0, D.jsx)(_t, { name: "model" }),
			/* @__PURE__ */ (0, D.jsx)("span", {
				id: "live-model",
				children: e.modelName()
			}),
			/* @__PURE__ */ (0, D.jsx)(_t, { name: "chevron" })
		]
	}), /* @__PURE__ */ (0, D.jsx)("button", {
		ref: t,
		type: "button",
		className: "task-menu-trigger context-menu-trigger",
		"data-telemetry-menu": "",
		"data-unknown": String(!r),
		"aria-haspopup": "dialog",
		"aria-expanded": !!n,
		"aria-controls": n?.panel.id,
		"aria-label": r ? `Context and usage: ${Math.round(i)}% context used` : "Context and usage: context unknown",
		title: "Context & usage",
		onClick: (t) => e.showMenu("telemetry", t.currentTarget),
		children: /* @__PURE__ */ (0, D.jsxs)("svg", {
			className: "context-ring",
			viewBox: "0 0 20 20",
			"aria-hidden": "true",
			children: [/* @__PURE__ */ (0, D.jsx)("circle", {
				className: "context-ring-track",
				cx: "10",
				cy: "10",
				r: "8",
				pathLength: "100"
			}), /* @__PURE__ */ (0, D.jsx)("circle", {
				className: "context-ring-value",
				cx: "10",
				cy: "10",
				r: "8",
				pathLength: "100",
				style: { strokeDasharray: `${i} 100` }
			})]
		})
	})] }) : /* @__PURE__ */ (0, D.jsx)("span", {
		className: "composer-model",
		id: "live-model",
		children: [e.snapshot.provider, e.snapshot.model].filter(Boolean).join(" / ")
	});
}
function Cr({ c: e }) {
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [e.policy && /* @__PURE__ */ (0, D.jsxs)("dialog", {
		ref: (t) => {
			e.policyDialog = t;
		},
		id: "workflow-permission-dialog",
		className: "folder-dialog permission-policy-dialog",
		"aria-labelledby": "workflow-permission-title",
		"aria-describedby": "workflow-permission-description",
		onClose: () => {
			e.policyTarget = null, e.consent = !1, e.publish();
		},
		onCancel: (t) => {
			t.preventDefault(), e.cancelPolicy();
		},
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-heading",
				children: [/* @__PURE__ */ (0, D.jsx)("h2", {
					id: "workflow-permission-title",
					children: "Allow tools without approval?"
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-policy-cancel": "",
					"aria-label": "Cancel permission change",
					onClick: e.cancelPolicy,
					children: /* @__PURE__ */ (0, D.jsx)(_t, { name: "close" })
				})]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				id: "workflow-permission-description",
				children: "Allow skips permission prompts for this session. Tools can modify files and run commands with your host account’s privileges. Snow does not provide a sandbox. Existing effects cannot be undone by switching back to Ask."
			}),
			/* @__PURE__ */ (0, D.jsxs)("label", {
				className: "checkbox-label",
				children: [/* @__PURE__ */ (0, D.jsx)("input", {
					type: "checkbox",
					"data-policy-ack": "",
					checked: e.consent,
					onChange: (t) => {
						e.consent = t.currentTarget.checked, e.publish();
					}
				}), " I understand the risks and want to enable Allow for this session."]
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-actions",
				children: [/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-policy-cancel": "",
					onClick: e.cancelPolicy,
					children: "Keep current policy"
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "button danger",
					"data-policy-confirm": "",
					disabled: !e.consent || !e.canSetPolicy(),
					onClick: () => void e.confirmPolicy(),
					children: "Enable Allow"
				})]
			})
		]
	}), e.workflow && /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("dialog", {
		ref: (t) => {
			e.switchDialog = t;
		},
		id: "workflow-switch-dialog",
		className: "folder-dialog",
		"aria-labelledby": "workflow-switch-title",
		"aria-describedby": "workflow-switch-description",
		onClose: () => {
			e.switchTarget = null;
		},
		onCancel: (t) => {
			t.preventDefault(), e.closeWorkflow("switch");
		},
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-heading",
				children: [/* @__PURE__ */ (0, D.jsx)("h2", {
					id: "workflow-switch-title",
					children: "Stop and switch conversation?"
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-workflow-cancel": "",
					"aria-label": "Cancel switching conversation",
					onClick: () => e.closeWorkflow("switch"),
					children: "×"
				})]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				id: "workflow-switch-description",
				children: "A turn or approval is active. Switching stops that work and opens the selected conversation in this project. Saved history and your unsent draft remain. Nothing is sent in the new conversation."
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-actions",
				children: [/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-workflow-cancel": "",
					onClick: () => e.closeWorkflow("switch"),
					children: "Keep working"
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "button danger",
					"data-workflow-switch-confirm": "",
					disabled: !e.canSwitch(),
					onClick: () => void e.confirmSwitch(),
					children: "Stop and switch"
				})]
			})
		]
	}), /* @__PURE__ */ (0, D.jsx)("dialog", {
		ref: (t) => {
			e.renameDialog = t;
		},
		id: "workflow-rename-dialog",
		className: "folder-dialog",
		"aria-labelledby": "workflow-rename-title",
		onCancel: (t) => {
			t.preventDefault(), e.closeWorkflow("rename");
		},
		children: /* @__PURE__ */ (0, D.jsxs)("form", {
			"data-workflow-rename-form": "",
			onSubmit: (t) => {
				t.preventDefault(), e.rename();
			},
			children: [
				/* @__PURE__ */ (0, D.jsxs)("div", {
					className: "dialog-heading",
					children: [/* @__PURE__ */ (0, D.jsx)("h2", {
						id: "workflow-rename-title",
						children: "Rename current conversation"
					}), /* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "quiet",
						"data-workflow-cancel": "",
						"aria-label": "Cancel renaming conversation",
						onClick: () => e.closeWorkflow("rename"),
						children: "×"
					})]
				}),
				/* @__PURE__ */ (0, D.jsx)("label", {
					htmlFor: "workflow-name",
					children: "Conversation name"
				}),
				/* @__PURE__ */ (0, D.jsx)("input", {
					ref: (t) => {
						e.nameInput = t;
					},
					id: "workflow-name",
					name: "name",
					maxLength: 128,
					required: !0,
					autoComplete: "off",
					value: e.renameDraft,
					onChange: (t) => {
						t.currentTarget.setCustomValidity(""), e.renameDraft = t.currentTarget.value, e.publish();
					}
				}),
				/* @__PURE__ */ (0, D.jsx)("p", {
					className: "fine",
					children: "Only the current conversation’s display name changes."
				}),
				/* @__PURE__ */ (0, D.jsxs)("div", {
					className: "dialog-actions",
					children: [/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "quiet",
						"data-workflow-cancel": "",
						onClick: () => e.closeWorkflow("rename"),
						children: "Cancel"
					}), /* @__PURE__ */ (0, D.jsx)("button", {
						type: "submit",
						className: "primary",
						disabled: !e.canChange(),
						children: "Save name"
					})]
				})
			]
		})
	})] })] });
}
//#endregion
//#region src/conversation/Menu.tsx
function wr({ label: e, rowKey: t = e, disabled: n = !1, checked: r, value: i, glyph: a, next: o, action: s, provider: c, model: l }) {
	return /* @__PURE__ */ (0, D.jsxs)("button", {
		type: "button",
		className: "snow-menu-row",
		"data-menu-key": t,
		role: r === void 0 ? "menuitem" : "menuitemradio",
		"aria-checked": r,
		disabled: n,
		title: c ? `${e} · ${c} / ${l}` : e + (i ? ` · ${i}` : ""),
		"data-model-provider": c,
		"data-model-id": l,
		onClick: s,
		children: [
			(a || t === "back") && /* @__PURE__ */ (0, D.jsx)(_t, { name: a || "back" }),
			/* @__PURE__ */ (0, D.jsx)("span", {
				className: "snow-menu-row-label",
				children: e
			}),
			i && /* @__PURE__ */ (0, D.jsx)("span", {
				className: "snow-menu-row-value",
				children: i
			}),
			o && /* @__PURE__ */ (0, D.jsx)(_t, { name: "next" }),
			r !== void 0 && /* @__PURE__ */ (0, D.jsx)("span", {
				className: "snow-menu-check",
				"aria-hidden": "true",
				children: /* @__PURE__ */ (0, D.jsx)("span", {
					style: { visibility: r ? "visible" : "hidden" },
					children: /* @__PURE__ */ (0, D.jsx)(_t, { name: "check" })
				})
			})
		]
	});
}
var Tr = ({ children: e }) => /* @__PURE__ */ (0, D.jsx)("p", {
	className: "snow-menu-note",
	children: e
}), Er = () => /* @__PURE__ */ (0, D.jsx)("div", {
	className: "snow-menu-separator",
	role: "separator"
});
function Dr({ c: e, menu: t }) {
	let n = gr(e.snapshot), r = hr(e.snapshot.telemetry);
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("h2", { children: "Context & usage" }), t.pane === "telemetry-details" ? /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsx)(wr, {
			label: "Back",
			rowKey: "back",
			action: () => e.pane("root")
		}),
		/* @__PURE__ */ (0, D.jsx)(Tr, { children: "Context estimates are approximate; last reported input is measured. An unknown context window cannot give a usage percentage. Unknown values are not zero." }),
		/* @__PURE__ */ (0, D.jsx)("dl", { children: /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "session-cost",
			children: [
				/* @__PURE__ */ (0, D.jsx)("dt", { children: "Recorded cost estimate" }),
				/* @__PURE__ */ (0, D.jsx)("dd", {
					"data-workflow-cost": "",
					"data-known": String(r.known),
					children: r.value
				}),
				/* @__PURE__ */ (0, D.jsx)("p", {
					className: "session-cost-note",
					children: "May exclude unpriced requests; aggregate currency coverage is not verified. An estimate, not a billing charge. Unknown is not zero."
				})
			]
		}) })
	] }) : /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("dl", { children: [
		/* @__PURE__ */ (0, D.jsxs)("div", {
			"data-menu-key": "workflowContext",
			children: [/* @__PURE__ */ (0, D.jsx)("dt", {
				"data-workflow-context-label": "",
				children: n.contextLabel
			}), /* @__PURE__ */ (0, D.jsx)("dd", {
				"data-workflow-context": "",
				children: n.context
			})]
		}),
		/* @__PURE__ */ (0, D.jsxs)("div", {
			"data-menu-key": "workflowUsage",
			children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Usage" }), /* @__PURE__ */ (0, D.jsx)("dd", {
				"data-workflow-usage": "",
				children: n.usage
			})]
		}),
		/* @__PURE__ */ (0, D.jsxs)("div", {
			"data-menu-key": "workflowCost",
			children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Cost estimate" }), /* @__PURE__ */ (0, D.jsx)("dd", {
				"data-workflow-cost": "",
				"data-known": String(n.knownCost),
				children: n.cost
			})]
		})
	] }), /* @__PURE__ */ (0, D.jsx)(wr, {
		label: "Details",
		rowKey: "telemetry-details",
		next: !0,
		action: () => e.pane("telemetry-details")
	})] })] });
}
function Or({ c: e, menu: t }) {
	let n = e.snapshot.instance_id;
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("p", {
		className: "snow-menu-group-label",
		children: t.pane === "permission-help" ? "Permission details" : "Session permissions"
	}), t.pane === "permission-help" ? /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsx)(wr, {
			label: "Back",
			rowKey: "back",
			action: () => e.pane("root")
		}),
		/* @__PURE__ */ (0, D.jsx)(Tr, { children: "Ask requests approval when required. Deny rejects non-read tools without asking. Allow skips permission prompts. Read-risk tools do not require approval." }),
		/* @__PURE__ */ (0, D.jsx)(Tr, { children: "These are approval policies, not sandbox modes. Tools run with the host account’s privileges. Changes affect this session only; existing session decisions still apply in Ask." })
	] }) : /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		Object.entries(or).map(([r, i]) => /* @__PURE__ */ (0, D.jsx)(wr, {
			rowKey: r,
			label: i,
			checked: e.snapshot.permission_mode === r,
			disabled: !e.canSetPolicy(),
			action: () => e.choosePolicy(r, t.trigger, n)
		}, r)),
		/* @__PURE__ */ (0, D.jsx)(Tr, { children: "Session only · not a sandbox." }),
		/* @__PURE__ */ (0, D.jsx)(wr, {
			label: "Details",
			rowKey: "permission-help",
			action: () => e.pane("permission-help")
		}),
		!e.canSetPolicy() && /* @__PURE__ */ (0, D.jsx)(Tr, { children: "Policy changes require a connected, verified, idle session." })
	] })] });
}
function kr({ c: e }) {
	let t = e.snapshot, n = t.instance_id;
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsx)("p", {
			className: "snow-menu-group-label",
			children: "Collaboration mode"
		}),
		[["default", "Default"], ["plan", "Plan Mode"]].map(([r, i]) => /* @__PURE__ */ (0, D.jsx)(wr, {
			label: i,
			rowKey: r,
			checked: t.mode === r,
			disabled: !e.canChange() || !["default", "plan"].includes(t.mode || ""),
			action: () => {
				e.valid(n) && e.canChange() && (e.snapshot.mode === r ? e.dismiss() : e.mutate(n, "mode", { mode: r }));
			}
		}, r)),
		/* @__PURE__ */ (0, D.jsx)(Tr, { children: t.mode === "plan" ? "Plan Mode: investigate and plan, not implement." : t.mode === "default" ? "Default mode is active in the runtime." : "Authoritative mode is unavailable." })
	] });
}
function Ar({ c: e, menu: t }) {
	let n = e.snapshot.instance_id, r = e.choices;
	if (t.pane === "root") {
		let r = e.runtimeActions();
		return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
			/* @__PURE__ */ (0, D.jsx)(wr, {
				label: "New conversation",
				rowKey: "new",
				glyph: "new",
				disabled: !e.canSwitch(),
				action: () => {
					e.valid(n) && e.switchTo("", t.trigger);
				}
			}),
			/* @__PURE__ */ (0, D.jsx)(wr, {
				label: "Rename conversation",
				rowKey: "rename",
				glyph: "rename",
				disabled: !e.canChange(),
				action: () => {
					e.valid(n) && e.openRename(t.trigger);
				}
			}),
			/* @__PURE__ */ (0, D.jsx)(Er, {}),
			/* @__PURE__ */ (0, D.jsx)(wr, {
				label: "Switch conversation",
				rowKey: "sessions",
				next: !0,
				disabled: !e.canSwitch(),
				action: () => e.pane("sessions")
			}),
			r.length > 0 && /* @__PURE__ */ (0, D.jsx)(Er, {}),
			r.map(({ key: t, label: r, source: i }) => /* @__PURE__ */ (0, D.jsx)(wr, {
				rowKey: t,
				label: r,
				glyph: t,
				disabled: i.disabled,
				action: () => {
					e.valid(n) && i.isConnected && !i.disabled && e.runtimeActions().some((e) => e.key === t && e.source === i) && (e.dismiss(), i.click());
				}
			}, t))
		] });
	}
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)(wr, {
		label: "Conversations",
		rowKey: "back",
		action: () => e.pane("root")
	}), r ? /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "snow-menu-groups",
			children: [!r.sessions.some((t) => t.session_id === e.snapshot.session_id) && /* @__PURE__ */ (0, D.jsx)(wr, {
				label: e.snapshot.session_name || "Untitled conversation",
				rowKey: "current",
				checked: !0,
				action: () => e.dismiss()
			}), r.sessions.map((r) => /* @__PURE__ */ (0, D.jsx)(wr, {
				rowKey: r.session_id,
				label: r.name || "Untitled conversation",
				checked: r.session_id === e.snapshot.session_id,
				disabled: !e.canSwitch(),
				action: () => {
					e.valid(n) && e.switchTo(r.session_id, t.trigger);
				}
			}, r.session_id))]
		}),
		r.sessions_available === !1 ? /* @__PURE__ */ (0, D.jsx)(Tr, { children: "Saved conversations are unavailable on this worker." }) : !r.sessions.length && /* @__PURE__ */ (0, D.jsx)(Tr, { children: "No other saved conversations." }),
		r.sessions_truncated && /* @__PURE__ */ (0, D.jsx)(Tr, { children: "Some conversations are omitted from this bounded list." })
	] }) : /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)(wr, {
		label: e.loading ? "Loading host choices…" : "Load conversations & models",
		rowKey: "load",
		glyph: "refresh",
		disabled: !e.canChange(),
		action: () => void e.load()
	}), /* @__PURE__ */ (0, D.jsx)("p", {
		className: "snow-menu-note",
		"data-workflow-load-status": "",
		role: "status",
		children: e.loadError || (e.loading ? "Contacting the host…" : "Loading may contact provider model discovery.")
	})] })] });
}
function jr({ c: e, menu: t }) {
	let n = e.choices, r = e.snapshot, i = r.instance_id, a = t.query.trim().toLocaleLowerCase(), o = n?.models.filter((e) => [
		e.provider,
		e.id,
		e.name
	].some((e) => e.toLocaleLowerCase().includes(a))) || [];
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("p", {
		className: "snow-menu-note",
		"data-workflow-load-status": "",
		role: "status",
		children: e.loading ? "Loading host models…" : e.loadError || (n ? "" : "Model discovery requires a connected, verified, idle session.")
	}), n && /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsx)("div", {
			className: "snow-menu-groups",
			role: "menu",
			"aria-label": "Available models",
			children: [...new Set(o.map((e) => e.provider))].map((t) => /* @__PURE__ */ (0, D.jsxs)("div", {
				className: "snow-menu-group",
				role: "group",
				"aria-label": t,
				children: [/* @__PURE__ */ (0, D.jsx)("p", {
					className: "snow-menu-group-label",
					title: t,
					children: t
				}), o.filter((e) => e.provider === t).map((t) => /* @__PURE__ */ (0, D.jsx)(wr, {
					rowKey: JSON.stringify([t.provider, t.id]),
					label: t.name || t.id,
					provider: t.provider,
					model: t.id,
					checked: t.provider === r.provider && t.id === r.model,
					disabled: !e.canChange(),
					action: () => {
						e.valid(i) && e.canChange() && e.choices?.models.some((e) => e.provider === t.provider && e.id === t.id) && (e.snapshot.provider === t.provider && e.snapshot.model === t.id ? e.dismiss() : e.mutate(i, "model", {
							provider: t.provider,
							model: t.id
						}));
					}
				}, JSON.stringify([t.provider, t.id])))]
			}, t))
		}),
		!o.length && /* @__PURE__ */ (0, D.jsx)("p", {
			className: "snow-menu-note",
			role: "status",
			children: a ? "No models match your search." : "No host models discovered."
		}),
		n.models_partial && /* @__PURE__ */ (0, D.jsx)(Tr, { children: "Some provider discovery was unavailable." }),
		n.models_truncated && /* @__PURE__ */ (0, D.jsx)(Tr, { children: "Some models are omitted from this bounded list." })
	] })] });
}
function Mr(e) {
	let { c: t, menu: n } = e;
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		n.kind === "model" && /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "snow-menu-header model-search-header",
			children: [/* @__PURE__ */ (0, D.jsx)("label", {
				className: "sr-only",
				htmlFor: "model-picker-search",
				children: "Search models by name, ID or provider"
			}), /* @__PURE__ */ (0, D.jsx)("input", {
				className: "model-search",
				type: "search",
				id: "model-picker-search",
				placeholder: "Search models…",
				maxLength: 256,
				autoComplete: "off",
				spellCheck: !1,
				"data-model-search": "",
				"data-menu-autofocus": "",
				value: n.query,
				onChange: (e) => t.search(e.currentTarget.value)
			})]
		}),
		/* @__PURE__ */ (0, D.jsx)("div", {
			className: "snow-menu-content",
			tabIndex: -1,
			children: n.kind === "telemetry" ? /* @__PURE__ */ (0, D.jsx)(Dr, { ...e }) : n.kind === "permission-policy" ? /* @__PURE__ */ (0, D.jsx)(Or, { ...e }) : n.kind === "mode" ? /* @__PURE__ */ (0, D.jsx)(kr, { ...e }) : n.kind === "session" ? /* @__PURE__ */ (0, D.jsx)(Ar, { ...e }) : /* @__PURE__ */ (0, D.jsx)(jr, { ...e })
		}),
		n.kind === "model" && /* @__PURE__ */ (0, D.jsx)("div", {
			className: "snow-menu-footer",
			role: "menu",
			"aria-label": "Model discovery",
			children: /* @__PURE__ */ (0, D.jsx)(wr, {
				label: t.loading ? t.choices ? "Refreshing models…" : "Loading models…" : t.loadError ? "Retry loading models" : "Refresh models",
				rowKey: "load",
				glyph: "refresh",
				disabled: !t.canChange(),
				action: () => void t.load()
			})
		})
	] });
}
//#endregion
//#region src/conversation/controller.tsx
var Nr = () => window.SnowMenus, Pr = null, Fr = 0, Ir = class {
	element;
	hooks;
	roots = /* @__PURE__ */ new Map();
	workflow;
	policy;
	projectName;
	projectPath;
	snapshot;
	controls = {};
	choices = null;
	sessionChoices = null;
	loading = !1;
	loadError = "";
	generation = 0;
	menu = null;
	observer = null;
	renameDialog = null;
	switchDialog = null;
	policyDialog = null;
	nameInput = null;
	renameInstance = "";
	renameSession = "";
	renameDraft = "";
	switchTarget = null;
	policyTarget = null;
	consent = !1;
	constructor(e, t) {
		this.element = e, this.hooks = t, this.workflow = e.dataset.workflowEnabled === "true", this.policy = e.dataset.permissionPolicyEnabled === "true", this.projectName = e.dataset.projectName || "", this.projectPath = e.dataset.projectPath || "", this.snapshot = {
			project_id: e.dataset.project || "",
			instance_id: e.dataset.instance || "",
			session_id: e.dataset.session || "",
			session_name: e.dataset.sessionName,
			provider: e.dataset.provider,
			model: e.dataset.model,
			status: e.dataset.status || "opening"
		};
		for (let t of [
			"heading",
			"leading",
			"trailing",
			"dialogs"
		]) {
			let n = e.querySelector(`[data-react-conversation="${t}"]`);
			n && this.roots.set(t, (0, d.createRoot)(n));
		}
	}
	current = () => Pr === this && this.element.isConnected;
	valid = (e) => this.current() && this.snapshot.instance_id === e;
	canChange = () => this.current() && !!this.controls.safe && !this.loading && this.snapshot.status === "idle";
	canSwitch = () => this.current() && !!this.controls.safe && !this.loading && (this.snapshot.status === "idle" || cr(this.snapshot.status));
	canSetPolicy = () => this.canChange() && sr(this.snapshot.permission_mode) && !this.snapshot.permission && !this.snapshot.input && !["admitted", "admission_unknown"].includes(this.snapshot.recovery?.state || "");
	modelName = () => this.choices?.models.find((e) => e.provider === this.snapshot.provider && e.id === this.snapshot.model)?.name || this.snapshot.model || "Model unknown";
	publish = () => {
		this.current() && ((0, u.flushSync)(() => {
			this.roots.get("heading")?.render(/* @__PURE__ */ (0, D.jsx)(br, { c: this })), this.roots.get("leading")?.render(/* @__PURE__ */ (0, D.jsx)(xr, { c: this })), this.roots.get("trailing")?.render(/* @__PURE__ */ (0, D.jsx)(Sr, { c: this })), this.roots.get("dialogs")?.render(/* @__PURE__ */ (0, D.jsx)(Cr, { c: this }));
		}), this.paintMenu());
	};
	paintMenu = () => {
		let e = this.menu;
		if (!e || !this.current()) return;
		let t = e.panel.contains(document.activeElement), n = document.activeElement instanceof HTMLElement ? document.activeElement : null, r = n?.dataset.menuKey, i = n?.matches("[data-model-search]"), a = String(e.kind !== "telemetry" && this.loading);
		if (e.panel.getAttribute("aria-busy") !== a && e.panel.setAttribute("aria-busy", a), e.kind === "telemetry") {
			let t = JSON.stringify([e.pane, gr(this.snapshot)]);
			if (e.signature === t) return;
			e.signature = t;
		}
		if ((0, u.flushSync)(() => e.root.render(/* @__PURE__ */ (0, D.jsx)(Mr, {
			c: this,
			menu: e
		}))), t && (!e.panel.contains(document.activeElement) || document.activeElement instanceof HTMLButtonElement && document.activeElement.disabled)) {
			let t = [...e.panel.querySelectorAll("button:not(:disabled)")], n = e.panel.querySelector("[data-model-search]");
			(i ? n : t.find((e) => e.dataset.menuKey === r) || n || t[0] || e.panel)?.focus({ preventScroll: !0 });
		}
		Nr()?.reposition();
	};
	dismiss = (e = !0) => {
		this.menu && Nr()?.close({ restoreFocus: e });
	};
	pane = (e) => {
		this.menu && (this.menu.pane = e, this.paintMenu(), this.menu?.panel.querySelector("button:not(:disabled)")?.focus());
	};
	search = (e) => {
		if (!this.menu) return;
		this.menu.query = e, this.paintMenu();
		let t = this.menu?.panel.querySelector(".snow-menu-content");
		t && (t.scrollTop = 0);
	};
	showMenu = (e, t) => {
		let n = Nr();
		if (!n || !this.current() || !t.isConnected) return;
		if (this.menu?.trigger === t) {
			this.dismiss();
			return;
		}
		n.close({ restoreFocus: !1 });
		let r = document.createElement("div");
		r.id = `snow-conversation-menu-${++Fr}`, r.className = `conversation-task-menu${e === "telemetry" ? " telemetry-menu" : ""}${e === "model" ? " model-picker-menu" : ""}`, r.setAttribute("aria-label", {
			model: "Model selection",
			session: "Conversation actions",
			mode: "Collaboration mode",
			"permission-policy": "Session permissions",
			telemetry: "Context and usage"
		}[e]), (e === "telemetry" || e === "model") && r.setAttribute("role", "dialog");
		let i = {
			kind: e,
			trigger: t,
			panel: r,
			root: (0, d.createRoot)(r),
			pane: "root",
			query: ""
		};
		this.menu = i, this.publish(), n.open({
			trigger: t,
			panel: r,
			managedTrigger: !0,
			placement: e === "session" ? "bottom-end" : "top-end",
			onClose: () => {
				this.menu === i && (this.menu = null), (0, u.flushSync)(() => i.root.unmount()), this.publish();
			},
			onBack: () => this.menu !== i || i.pane === "root" ? !1 : (this.pane("root"), !0)
		}), e === "model" && !this.choices && !this.loading && this.load();
	};
	runtimeActions = () => [
		[
			"versions",
			"Conversation versions",
			"[data-versions-open]"
		],
		[
			"goals",
			"Thread goal",
			"[data-goals-open]"
		],
		[
			"processes",
			"Managed processes",
			"[data-processes-open]"
		],
		[
			"reasoning",
			"Thinking & response",
			"[data-reasoning-open]"
		],
		[
			"compaction",
			"Compact context",
			"[data-compaction-open]"
		],
		[
			"steer",
			"Steer current run…",
			"[data-steer-open]"
		]
	].flatMap(([e, t, n]) => {
		let r = this.element.querySelector(n);
		return r && (!r.hidden || e === "goals" && r.dataset.goalsAvailable === "true") ? [{
			key: e,
			label: t,
			source: r
		}] : [];
	});
	mutate = async (e, t, n) => {
		this.valid(e) && this.canChange() && (this.dismiss(), await this.hooks.action(t, n));
	};
	load = async () => {
		if (!this.canChange()) return;
		this.loading = !0, this.loadError = "";
		let e = ++this.generation, { instance_id: t, project_id: n } = this.snapshot;
		this.publish();
		try {
			let r = await this.hooks.choices();
			if (!this.valid(t) || e !== this.generation) return;
			let i = pr(r, t, n);
			if (!i) throw Error("Invalid choices");
			this.choices = i, this.sessionChoices = i.sessions;
		} catch {
			this.current() && e === this.generation && (this.loadError = "Host choices unavailable. Try again.");
		} finally {
			this.current() && e === this.generation && (this.loading = !1, this.publish());
		}
	};
	switchTo = async (e, t, n = !1) => this.canSwitch() ? e === this.snapshot.session_id ? (this.dismiss(), !0) : e && !n && !(this.sessionChoices || this.choices?.sessions)?.some((t) => t.session_id === e) ? !1 : (this.dismiss(), cr(this.snapshot.status) ? this.switchDialog ? (this.switchTarget = {
		sessionID: e,
		instance: this.snapshot.instance_id
	}, this.publish(), this.hooks.openDialog(this.switchDialog, t), !0) : !1 : await this.hooks.action("switch", { session_id: e })) : !1;
	openRename = (e) => {
		this.canChange() && e?.isConnected && this.renameDialog && (this.dismiss(!1), this.renameInstance = this.snapshot.instance_id, this.renameSession = this.snapshot.session_id, this.renameDraft = this.snapshot.session_name || "", this.publish(), this.nameInput?.setCustomValidity(""), this.hooks.openDialog(this.renameDialog, e));
	};
	rename = async () => {
		if (!this.canChange() || this.renameInstance !== this.snapshot.instance_id || this.renameSession !== this.snapshot.session_id || !this.renameDialog?.open) return;
		let e = this.renameDraft.trim();
		if (!lr(e)) {
			this.nameInput?.setCustomValidity("Enter a name of at most 256 UTF-8 bytes."), this.nameInput?.reportValidity();
			return;
		}
		let t = this.renameDialog, n = this.snapshot.instance_id;
		await this.hooks.action("rename", { name: e }), this.valid(n) && this.hooks.closeDialog(t);
	};
	closeWorkflow = (e) => {
		this.switchTarget = null;
		let t = e === "switch" ? this.switchDialog : this.renameDialog;
		t && this.hooks.closeDialog(t);
	};
	confirmSwitch = async () => {
		let e = this.switchTarget;
		this.switchDialog?.open && this.canSwitch() && e && e.instance === this.snapshot.instance_id && (this.switchTarget = null, this.hooks.closeDialog(this.switchDialog), await this.hooks.action("switch", {
			session_id: e.sessionID,
			confirm_stop: "stop"
		}));
	};
	choosePolicy = (e, t, n) => {
		if (this.valid(n) && this.canSetPolicy() && sr(e)) {
			if (this.snapshot.permission_mode === e) {
				this.dismiss();
				return;
			}
			if (e !== "allow") {
				this.mutate(n, "permission-mode", {
					mode: e,
					session_id: this.snapshot.session_id
				});
				return;
			}
			this.dismiss(), this.policyDialog && (this.policyTarget = {
				instance: n,
				session: this.snapshot.session_id,
				previous: this.snapshot.permission_mode
			}, this.consent = !1, this.publish(), this.hooks.openDialog(this.policyDialog, t));
		}
	};
	cancelPolicy = () => {
		this.policyTarget = null, this.consent = !1, this.policyDialog && this.hooks.closeDialog(this.policyDialog), this.publish();
	};
	confirmPolicy = async () => {
		let e = this.policyTarget;
		e && this.policyDialog?.open && this.consent && this.canSetPolicy() && e.instance === this.snapshot.instance_id && e.session === this.snapshot.session_id && e.previous === this.snapshot.permission_mode && (this.cancelPolicy(), await this.hooks.action("permission-mode", {
			mode: "allow",
			session_id: e.session,
			confirm_allow: "allow"
		}));
	};
};
function Lr(e, t) {
	if (zr(), !e?.querySelector("[data-react-conversation]")) return;
	let n = Pr = new Ir(e, t);
	n.publish(), n.observer = new MutationObserver(() => {
		n.current() && n.menu?.kind === "session" && n.paintMenu();
	});
	let r = e.querySelector(".live-manager-controls");
	r && n.observer.observe(r, {
		subtree: !0,
		childList: !0,
		attributes: !0,
		attributeFilter: ["disabled", "hidden"]
	});
}
function Rr(e, t = {}) {
	let n = Pr;
	if (!n?.current()) return;
	n.controls = t, e && (n.snapshot.instance_id !== e.instance_id || n.snapshot.project_id !== e.project_id ? (n.choices = null, n.sessionChoices = null, n.loading = !1, n.generation++, n.loadError = "", n.switchTarget = null, n.dismiss(!1), n.switchDialog && n.hooks.closeDialog(n.switchDialog), n.renameDialog && n.hooks.closeDialog(n.renameDialog)) : n.snapshot.session_id !== e.session_id && (n.dismiss(!1), n.switchTarget = null, n.switchDialog && n.hooks.closeDialog(n.switchDialog), n.renameDialog && n.hooks.closeDialog(n.renameDialog)), n.snapshot = e), (!n.canSetPolicy() && n.menu?.kind === "permission-policy" || !n.canChange() && n.menu?.kind === "mode") && n.dismiss(!1);
	let r = n.policyTarget;
	r && (!n.canSetPolicy() || r.instance !== n.snapshot.instance_id || r.session !== n.snapshot.session_id || r.previous !== n.snapshot.permission_mode) && n.cancelPolicy(), n.publish();
}
function zr() {
	let e = Pr;
	if (e) {
		e.dismiss(!1), Pr = null, e.observer?.disconnect(), e.generation++;
		for (let t of [
			e.renameDialog,
			e.switchDialog,
			e.policyDialog
		]) t && e.hooks.closeDialog(t);
		(0, u.flushSync)(() => {
			for (let t of e.roots.values()) t.unmount();
		});
	}
}
async function Br({ project: e, session: t = "", instance: n, trigger: r }) {
	let i = Pr;
	if (!i?.canSwitch() || i.snapshot.project_id !== e || n && i.snapshot.instance_id !== n) return !1;
	let a = i.snapshot.instance_id;
	if (t && t !== i.snapshot.session_id) {
		if (r?.dataset.project === e && r.dataset.instance === a && r.closest("[data-shell-session]")?.dataset.shellSession === t) return await i.switchTo(t, r, !0);
		if (cr(i.snapshot.status) || !i.hooks.sessions) return !1;
		i.loading = !0;
		let n = ++i.generation;
		i.publish();
		try {
			let r = await i.hooks.sessions(t);
			if (!i.valid(a) || n !== i.generation || !ur(r) || r.project_id !== e || r.instance_id !== a || !r.available) return !1;
			let o = fr(r.sessions);
			if (!o) return !1;
			i.sessionChoices = o;
		} catch {
			return !1;
		} finally {
			i.current() && n === i.generation && (i.loading = !1, i.publish());
		}
		if (!i.canSwitch() || !i.sessionChoices?.some((e) => e.session_id === t)) return !1;
	}
	return i.valid(a) ? await i.switchTo(t, r) : !1;
}
var Vr = Object.freeze({
	init: Lr,
	render: Rr,
	dispose: zr,
	select: Br,
	rename: (e) => Pr?.openRename(e)
}), Hr = new TextEncoder(), Ur = /* @__PURE__ */ new Set([
	"accepted",
	"delivered",
	"discarded",
	"uncertain"
]), Wr = {
	accepted: "Accepted · awaiting native delivery",
	delivered: "Delivered · confirmed by native run",
	discarded: "Discarded · confirmed by native run",
	uncertain: "Outcome uncertain · may still be delivered; review before submitting again"
};
function Gr(e) {
	return !!e && typeof e == "object" && !Array.isArray(e);
}
function Kr(e) {
	return typeof e == "string" && e.length > 0 && e.trim() === e && Hr.encode(e).length <= 256 && !/[\x00-\x1f\x7f]/.test(e);
}
function qr(e = globalThis.crypto) {
	if (typeof e.randomUUID == "function") return e.randomUUID();
	let t = /* @__PURE__ */ new Uint8Array(16);
	e.getRandomValues(t), t[6] = t[6] & 15 | 64, t[8] = t[8] & 63 | 128;
	let n = Array.from(t, (e) => e.toString(16).padStart(2, "0")).join("");
	return `${n.slice(0, 8)}-${n.slice(8, 12)}-${n.slice(12, 16)}-${n.slice(16, 20)}-${n.slice(20)}`;
}
function Jr(e, t) {
	if (!Gr(e) || typeof e.live_steer_token != "string" || e.live_steer_token !== "" && !Kr(e.live_steer_token) || !Number.isSafeInteger(e.revision) || e.revision < 0 || typeof e.can_steer != "boolean" || !Array.isArray(e.items) || e.items.length > 8 || e.can_steer && (!Kr(e.live_steer_token) || e.revision <= 0)) return !1;
	let n = 0, r = /* @__PURE__ */ new Set(), i = /* @__PURE__ */ new Set();
	for (let a of e.items) {
		if (!Gr(a) || !Kr(a.request_id) || r.has(a.request_id) || !Ur.has(a.status) || typeof a.text != "string" || !t.validText(a.text) || a.item_id !== void 0 && a.item_id !== "" && (!Kr(a.item_id) || i.has(a.item_id)) || a.status !== "uncertain" && !Kr(a.item_id)) return !1;
		r.add(a.request_id), typeof a.item_id == "string" && a.item_id && i.add(a.item_id), n += Hr.encode(a.text).length;
	}
	return n <= 262144;
}
function Yr(e, t) {
	return Gr(e) && e.project_id === t.project_id && e.instance_id === t.instance_id && e.session_id === t.session_id;
}
function Xr(e) {
	return {
		store: e,
		ui: null,
		projection: null,
		snapshotRevision: -1,
		invalid: !1,
		composing: !1,
		opened: !1
	};
}
function Zr(e, t, n, r, i) {
	e.ui = { ...n }, !(!t || !Number.isSafeInteger(t.revision) || t.revision < e.snapshotRevision) && (e.snapshotRevision = t.revision, e.invalid = [
		"project_id",
		"instance_id",
		"session_id"
	].some((e) => {
		let n = e;
		return t[n] !== void 0 && t[n] !== i[n];
	}) || t.steer != null && !Jr(t.steer, r), e.projection = !e.invalid && Jr(t.steer, r) ? {
		...t.steer,
		items: t.steer.items.map((e) => ({ ...e }))
	} : null);
}
function Qr(e) {
	return !!(e.opened || e.store.busy || e.store.unknown);
}
function $r(e) {
	let t = e.ui;
	return !!t && t.connected && !t.busy && !t.unknown && !t.stopping && !t.editing && t.status === "running" && !e.invalid && e.projection?.can_steer === !0 && !e.store.busy;
}
function ei(e) {
	return $r(e) && !e.store.unknown;
}
function ti(e) {
	let t = e.ui;
	return !!t && t.connected && !t.busy && !t.stopping && !t.editing && t.status === "idle" && !e.invalid && !!e.store.unknown && !e.store.busy;
}
function ni(e) {
	return !!e.projection && (!e.store.token || e.store.token !== e.projection.live_steer_token || e.store.baseRevision !== e.projection.revision);
}
function ri(e) {
	return !$r(e) || !e.projection ? !1 : (e.store.token = e.projection.live_steer_token, e.store.baseRevision = e.projection.revision, !0);
}
function ii(e, t) {
	Hr.encode(t).length > 65536 ? e.error = "Steering is limited to 64 KiB; your previous draft is kept." : (e.text = t, e.revision++);
}
function ai(e, t, n) {
	if (!Gr(e) || !Yr(e, t.identity) || !Gr(e.steer_ack) || !Number.isSafeInteger(e.revision) || e.revision <= 0) return !1;
	let r = e.steer_ack;
	return r.live_steer_token === t.token && r.request_id === t.request && Kr(r.item_id) && r.status === "accepted" && Jr(e.steer, n);
}
function oi(e, t) {
	e.error = "Accepted by the native run. Acceptance is not delivery; see the steering history for its confirmed outcome.", e.revision === t.draftRevision && e.text === t.text && (e.text = "", e.revision++), e.unknown = !1;
}
//#endregion
//#region src/steer/SteerPanel.tsx
function si({ state: e, actions: t, dialog: n, input: r }) {
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsx)("button", {
			type: "button",
			className: "quiet",
			"data-steer-open": "",
			hidden: e.triggerHidden,
			disabled: !e.canOpen,
			title: "Direct the current run at its next safe native boundary. Queue next is a separate follow-up.",
			onClick: (e) => {
				e.preventDefault(), t.open(e.currentTarget);
			},
			children: "Steer current run…"
		}),
		/* @__PURE__ */ (0, D.jsxs)("section", {
			id: "live-steer-history",
			className: "steer-history",
			"aria-label": "Current-run steering",
			hidden: !e.items.length,
			children: [
				/* @__PURE__ */ (0, D.jsx)("h3", { children: "Current-run steering" }),
				/* @__PURE__ */ (0, D.jsx)("p", {
					className: "muted",
					children: "Acceptance is not delivery. Only native delivery or discard confirms the outcome."
				}),
				/* @__PURE__ */ (0, D.jsx)("ol", {
					"data-steer-items": "",
					children: e.items.map((n) => /* @__PURE__ */ (0, D.jsxs)("li", {
						"data-steer-request": n.request_id,
						children: [
							/* @__PURE__ */ (0, D.jsx)("p", {
								"data-steer-state": "",
								children: Wr[n.status]
							}),
							/* @__PURE__ */ (0, D.jsx)("pre", {
								"data-steer-source": "",
								children: n.text
							}),
							/* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "quiet",
								"data-steer-copy": "",
								disabled: e.busy,
								onClick: (e) => {
									e.preventDefault(), t.copy(n.request_id, e.currentTarget);
								},
								children: "Review text"
							})
						]
					}, n.request_id))
				})
			]
		}),
		/* @__PURE__ */ (0, D.jsxs)("dialog", {
			ref: n,
			id: "live-steer-dialog",
			className: "steer-dialog runtime-dialog",
			"aria-labelledby": "steer-heading",
			"aria-describedby": "steer-explanation",
			onCancel: (e) => {
				e.preventDefault(), t.close();
			},
			onClose: t.closed,
			children: [/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-heading",
				children: [/* @__PURE__ */ (0, D.jsx)("h2", {
					id: "steer-heading",
					children: "Steer current run"
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet icon-button",
					"data-steer-close": "",
					"aria-label": "Close steering and keep draft",
					title: "Close",
					onClick: t.close,
					children: /* @__PURE__ */ (0, D.jsx)("svg", {
						className: "icon",
						width: "18",
						height: "18",
						viewBox: "0 0 24 24",
						fill: "none",
						stroke: "currentColor",
						strokeWidth: "1.7",
						strokeLinecap: "round",
						"aria-hidden": "true",
						children: /* @__PURE__ */ (0, D.jsx)("path", { d: "m6 6 12 12M6 18 18 6" })
					})
				})]
			}), /* @__PURE__ */ (0, D.jsxs)("form", {
				className: "runtime-dialog-body",
				"data-steer-form": "",
				onSubmit: (e) => {
					e.preventDefault(), e.stopPropagation(), t.submit();
				},
				children: [
					/* @__PURE__ */ (0, D.jsx)("p", {
						id: "steer-explanation",
						children: "Send a correction or direction into the current run. Snow delivers literal text at a safe boundary after the current assistant response or tool batch, not in the middle of a tool. Provider failure can still be followed by delivery."
					}),
					/* @__PURE__ */ (0, D.jsxs)("p", {
						className: "muted",
						children: [/* @__PURE__ */ (0, D.jsx)("strong", { children: "Queue next" }), " is different: it schedules a natural follow-up after current work. Steering does not create a separate queued follow-up or an optimistic chat message."]
					}),
					/* @__PURE__ */ (0, D.jsx)("label", {
						htmlFor: "live-steer-text",
						children: "Direction for this run"
					}),
					/* @__PURE__ */ (0, D.jsx)("textarea", {
						ref: r,
						id: "live-steer-text",
						"data-steer-text": "",
						rows: 6,
						placeholder: "For the current task, focus on…",
						"aria-describedby": "steer-draft-hint",
						spellCheck: !0,
						value: e.text,
						readOnly: e.busy,
						onChange: (e) => t.text(e.currentTarget.value),
						onCompositionStart: () => t.compose(!0),
						onCompositionEnd: () => t.compose(!1),
						onKeyDown: (n) => {
							n.key === "Enter" && (n.ctrlKey || n.metaKey) && !n.nativeEvent.isComposing && !e.composing && (n.preventDefault(), t.submit());
						}
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						id: "steer-draft-hint",
						className: "muted",
						children: "Up to 64 KiB. Enter adds a line; Ctrl/⌘ + Enter sends. Closing keeps this draft in this tab."
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						"data-steer-error": "",
						role: "status",
						"aria-live": "polite",
						hidden: !e.error,
						children: e.error
					}),
					/* @__PURE__ */ (0, D.jsxs)("footer", { children: [/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "quiet",
						"data-steer-review": "",
						hidden: e.reviewHidden,
						disabled: !e.canReview,
						onClick: t.review,
						children: e.dismiss ? "Keep draft and dismiss" : "Review current run"
					}), /* @__PURE__ */ (0, D.jsx)("button", {
						type: "submit",
						"data-steer-submit": "",
						disabled: !e.canSubmit,
						children: e.busy ? "Sending steering…" : "Send steering"
					})] })
				]
			})]
		})
	] });
}
//#endregion
//#region src/steer/controller.tsx
var ci = /* @__PURE__ */ new Map(), I = null;
function li(e) {
	ui();
	let t = e.root.querySelector("[data-react-live-panel=\"steer\"]");
	if (!t) return;
	let n = Object.freeze({
		project_id: e.root.dataset.project || "",
		session_id: e.root.dataset.session || "",
		instance_id: e.root.dataset.instance || ""
	});
	if (e.session !== n.session_id || e.identity && !Yr(e.identity, n)) return;
	let r = JSON.stringify([
		n.project_id,
		n.session_id,
		n.instance_id
	]), i = ci.get(r) || {
		text: "",
		revision: 0
	};
	for (ci.delete(r), ci.set(r, i); ci.size > 16;) {
		let e = ci.keys().next();
		e.done || ci.delete(e.value);
	}
	let a = {
		api: e,
		identity: n,
		container: t,
		root: (0, d.createRoot)(t),
		state: Xr(i),
		present: !0,
		dialog: (0, l.createRef)(),
		input: (0, l.createRef)(),
		returnFocus: null,
		actions: {
			open: (e) => {
				di(a) && yi(e);
			},
			close: () => Si(a),
			closed: () => bi(a),
			review: () => Ci(a),
			copy: (e, t) => wi(a, e, t),
			text: (e) => {
				di(a) && (ii(i, e), gi(a));
			},
			compose: (e) => {
				di(a) && (a.state.composing = e, gi(a));
			},
			submit: () => {
				Ti(a);
			}
		}
	};
	I = a, gi(a);
}
function ui() {
	if (!I) return;
	let e = I;
	I = null, e.state.store.busy && (e.state.store.busy = null, e.state.store.unknown = !0, e.state.store.error = "The steering result is unknown. Your draft is kept; nothing is retried automatically."), e.state.opened = !1, e.dialog.current?.open && (e.api.closeDialog ? e.api.closeDialog(e.dialog.current) : e.dialog.current.close()), (0, u.flushSync)(() => e.root.unmount());
}
function di(e) {
	let { api: t, identity: n } = e;
	return I === e && t.root.isConnected && t.root.contains(e.container) && t.session === n.session_id && t.root.dataset.project === n.project_id && t.root.dataset.instance === n.instance_id && t.root.dataset.session === n.session_id;
}
function fi() {
	return !!I && Qr(I.state);
}
function pi() {
	return !!I && di(I) && ei(I.state);
}
function mi(e, t, n = !0) {
	if (!I) return;
	let r = I;
	Zr(r.state, e, t, r.api, r.identity), r.present = n, n && gi(r);
}
function hi(e) {
	let { state: t } = e, { store: n } = t, r = ni(t), i = di(e) && $r(t), a = di(e) && ti(t);
	return {
		text: n.text,
		busy: !!n.busy,
		composing: t.composing,
		triggerHidden: !t.projection && !t.invalid && !n.unknown,
		canOpen: di(e) && (ei(t) || !!n.unknown && (i || a)),
		canSubmit: di(e) && ei(t) && !r && !t.composing && e.api.validText(n.text),
		error: n.error || (r ? "The reviewed run changed. Your draft is kept. Review the current run again before sending." : !i && t.opened ? "Steering is unavailable while the run is stopped, disconnected, needs attention, or another control owns it. Your draft is kept." : ""),
		reviewHidden: !(n.unknown || r || n.rejected),
		canReview: i || a,
		dismiss: a,
		items: t.projection?.items || []
	};
}
function gi(e) {
	if (I !== e || !e.present) return;
	let t = hi(e);
	(0, u.flushSync)(() => e.root.render(/* @__PURE__ */ (0, D.jsx)(si, {
		state: t,
		actions: e.actions,
		dialog: e.dialog,
		input: e.input
	})));
}
function _i(e) {
	I === e && (e.api.changed(), gi(e));
}
function vi(e, t) {
	let n = e.dialog.current;
	if (!n) return !1;
	e.returnFocus = t || e.api.root.ownerDocument.activeElement, e.state.opened = !0;
	try {
		n.open || (e.api.openDialog ? e.api.openDialog(n, e.returnFocus) : n.showModal());
	} catch {
		return e.state.opened = !1, _i(e), !1;
	}
	return !0;
}
function yi(e) {
	if (!I || !di(I)) return !1;
	let t = I, n = t.state;
	return !ei(n) && !(n.store.unknown && ($r(n) || ti(n))) || !n.store.unknown && !ri(n) || (n.store.unknown || (n.store.error = ""), !vi(t, e)) ? !1 : (t.input.current?.focus(), _i(t), !0);
}
function bi(e) {
	I === e && !e.dialog.current?.open && e.state.opened && (e.state.opened = !1, _i(e), xi(e));
}
function xi(e) {
	!e.api.openDialog && e.returnFocus?.isConnected && !e.returnFocus.matches(":disabled") && e.returnFocus.focus({ preventScroll: !0 });
}
function Si(e) {
	I === e && (e.state.opened = !1, e.dialog.current?.open && (e.api.closeDialog ? e.api.closeDialog(e.dialog.current) : e.dialog.current.close()), _i(e), xi(e));
}
function Ci(e) {
	if (!di(e)) return;
	let t = e.state, { store: n } = t;
	ti(t) ? (n.unknown = !1, n.rejected = !1, n.token = "", n.baseRevision = 0, n.error = "Draft kept. The earlier steering outcome is still uncertain unless native history confirms it. Dismissal does not mean delivery or discard; nothing is retried automatically.", Si(e)) : ri(t) && (n.unknown = !1, n.rejected = !1, n.error = "Reviewed the current run. Sending again is a new explicit request, not a retry; an uncertain earlier request may already have been delivered.", _i(e));
}
function wi(e, t, n) {
	if (!di(e) || e.state.store.busy) return;
	let { store: r } = e.state, i = e.state.projection?.items.find((e) => e.request_id === t);
	i && (r.text && r.text !== i.text ? r.error = "Your steering draft is kept. Copy the history text manually to avoid replacing it." : (r.text = i.text, r.revision++), vi(e, n) && _i(e));
}
async function Ti(e) {
	let { state: t, api: n } = e, { store: r } = t;
	if (!di(e) || !ei(t) || t.composing || !n.validText(r.text) || ni(t) || !t.projection) return !1;
	let i = Object.freeze({
		identity: e.identity,
		token: t.projection.live_steer_token,
		revision: t.projection.revision,
		request: qr(),
		text: r.text,
		draftRevision: r.revision
	});
	r.busy = i, r.error = "", r.rejected = !1, _i(e);
	try {
		if (!di(e) || r.busy !== i) return !1;
		let t = await n.request("steer", {
			session_id: i.identity.session_id,
			live_steer_token: i.token,
			steer_revision: String(i.revision),
			request_id: i.request,
			text: i.text
		});
		if (I !== e || r.busy !== i) return !1;
		if (!di(e) || !ai(t, i, n)) throw Error("Unverified steering receipt");
		return oi(r, i), !0;
	} catch {
		return I !== e || r.busy !== i ? !1 : (r.unknown = !0, r.error = "Steering was rejected or its outcome is unknown. Your draft is kept. Review the current run and native history before any new explicit submission; nothing is retried automatically.", !1);
	} finally {
		I === e && r.busy === i && (r.busy = null, _i(e));
	}
}
var Ei = Object.freeze({
	init: li,
	dispose: ui,
	render: mi,
	blocking: fi,
	canSteer: pi,
	open: yi
});
function Di(e, t) {
	if (!e || typeof e != "object") return !1;
	let n = e;
	if (typeof n.token != "string" || n.token.length > 256 || !Number.isSafeInteger(n.revision) || n.revision < 0 || typeof n.can_enqueue != "boolean" || !Array.isArray(n.items) || n.items.length > 8) return !1;
	let r = /* @__PURE__ */ new Set(), i = 0;
	for (let e of n.items) {
		if (!e || typeof e.id != "string" || !e.id || e.id.length > 256 || r.has(e.id) || ![
			"pending",
			"starting",
			"held",
			"uncertain"
		].includes(e.state) || typeof e.text != "string" || !t(e.text)) return !1;
		r.add(e.id), i += new TextEncoder().encode(e.text).length;
	}
	return i <= 262144;
}
function Oi(e, t) {
	return e.value === t.value && e.revision === t.revision && e.start === t.start && e.end === t.end && e.direction === t.direction;
}
function ki(e) {
	return !!e?.items.some((e) => ["held", "uncertain"].includes(e.state));
}
function Ai(e) {
	let { ui: t, store: n, queue: r, invalid: i } = e;
	return !!t && t.connected && !t.busy && !t.unknown && !t.stopping && !t.editing && !i && !n.busy && !n.unknown && !!r?.token;
}
function ji(e) {
	return Ai(e) && !e.composing && e.ui?.status === "running" && e.queue?.can_enqueue === !0 && e.queue.items.length < 8;
}
function Mi(e) {
	return !!(e.store.busy || e.store.unknown || e.invalid || ki(e.queue));
}
function Ni(e) {
	let { ui: t, store: n, queue: r } = e, i = ji(e), a = ![
		"running",
		"permission",
		"input"
	].includes(t?.status || "");
	return {
		hidden: a,
		disabled: !i,
		label: n.busy && n.busy.action === "queue-enqueue" ? "Queuing…" : "Queue next",
		title: i ? "Queue this draft after the current work; it is not sent immediately" : "Queue next needs a connected running turn accepting follow-ups; approval and question takeovers pause queuing",
		hint: a ? "Ctrl / ⌘ + Enter to send · Enter for a new line" : i ? "Ctrl / ⌘ + Enter to queue next · Enter for a new line" : "Queue next is paused · your draft is kept",
		state: a ? ki(r) ? "Remove reviewed pending items before starting or switching conversations" : null : n.unknown ? "Review the queue request outcome; nothing will retry" : n.busy ? "Updating pending messages · Stop remains available" : r?.items.some((e) => e.state === "pending") ? "Pending messages are separate from your draft" : "Turn in progress · Queue next is an explicit action"
	};
}
function Pi(e, t, n, r) {
	return Di(e, r) && e.token === t.token && n?.token === t.token && e.revision > t.revision;
}
//#endregion
//#region src/queue/QueuePanel.tsx
var Fi = {
	pending: "Pending · after current work",
	starting: "Delivery starting · locked",
	held: "Held for review · not scheduled",
	uncertain: "Delivery unknown · may already have been delivered",
	missing: "No longer pending · review the conversation; your edit is kept"
};
function Ii({ item: e, current: t, rowRefs: n, onAction: r, onText: i, onComposition: a }) {
	let { store: o, queue: s, ui: c } = t, l = o.editors.get(e.id), u = e.state === "pending" && Ai(t) && c?.status === "running" && (!l || l.token === s?.token), d = ["held", "uncertain"].includes(e.state), f = n.get(e.id);
	return f || (f = {
		input: null,
		edit: null
	}, n.set(e.id, f)), /* @__PURE__ */ (0, D.jsxs)("article", {
		className: "queue-next-item",
		"data-queue-item-id": e.id,
		"data-queue-state": e.state,
		children: [
			/* @__PURE__ */ (0, D.jsx)("div", {
				className: "queue-next-item-heading",
				children: /* @__PURE__ */ (0, D.jsx)("span", {
					className: "queue-next-item-state",
					"data-queue-item-state": "",
					children: Fi[e.state]
				})
			}),
			/* @__PURE__ */ (0, D.jsx)("pre", {
				className: "queue-next-source",
				"data-queue-source": "",
				children: e.text
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "queue-next-actions",
				children: [
					/* @__PURE__ */ (0, D.jsx)("button", {
						ref: (e) => {
							f.edit = e;
						},
						type: "button",
						className: "quiet",
						"data-queue-edit": "",
						hidden: !!l || e.state !== "pending",
						disabled: !u,
						onClick: () => r("edit", e.id),
						children: "Edit"
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "quiet",
						"data-queue-remove": "",
						hidden: e.state !== "pending" && !d,
						disabled: !!l || !(u || d && Ai(t)),
						title: d ? "Remove from this review list only; this does not undo or revoke delivery" : "Remove this pending message before delivery starts",
						onClick: () => r("remove", e.id),
						children: "Remove"
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "quiet",
						"data-queue-copy-draft": "",
						"data-queue-copy": "",
						hidden: ![
							"held",
							"uncertain",
							"missing"
						].includes(e.state),
						disabled: !!o.busy || !!c?.editing,
						onClick: () => r("copy", e.id),
						children: "Copy to draft"
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsxs)("form", {
				className: "queue-next-editor",
				"data-queue-editor": "",
				noValidate: !0,
				hidden: !l,
				onSubmit: (t) => {
					t.preventDefault(), t.stopPropagation(), r("save", e.id);
				},
				children: [
					/* @__PURE__ */ (0, D.jsx)("textarea", {
						ref: (e) => {
							f.input = e;
						},
						"data-queue-text": "",
						maxLength: 65536,
						rows: 3,
						"aria-label": "Edit pending message",
						value: l?.text || "",
						onChange: (t) => i(e.id, t.currentTarget.value),
						onCompositionStart: () => a(e.id, !0),
						onCompositionEnd: () => a(e.id, !1)
					}),
					/* @__PURE__ */ (0, D.jsxs)("div", {
						className: "queue-next-actions",
						children: [/* @__PURE__ */ (0, D.jsx)("button", {
							type: "submit",
							className: "quiet",
							"data-queue-save": "",
							disabled: !u || !!l?.composing,
							children: "Save"
						}), /* @__PURE__ */ (0, D.jsx)("button", {
							type: "button",
							className: "quiet",
							"data-queue-cancel": "",
							disabled: !!o.busy && o.busy.id === e.id,
							onClick: () => r("cancel", e.id),
							children: "Cancel"
						})]
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						className: "error",
						"data-queue-editor-error": "",
						role: "alert",
						hidden: !l?.error,
						children: l?.error || ""
					})
				]
			})
		]
	});
}
function Li(e) {
	let { current: t, panelRef: n, onAction: r } = e, { store: i, queue: a, ui: o, invalid: s } = t, c = [...a?.items || []], l = new Set(c.map((e) => e.id));
	for (let [e, t] of i.editors) l.has(e) || c.push({
		id: e,
		text: t.text,
		state: "missing"
	});
	let u = !c.length && !i.unknown && !i.error && !i.copiedDraft && !s;
	return /* @__PURE__ */ (0, D.jsxs)("section", {
		ref: n,
		id: "live-queue-next",
		className: "queue-next-panel",
		"aria-labelledby": "queue-next-heading",
		tabIndex: -1,
		hidden: u,
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "queue-next-heading",
				children: [/* @__PURE__ */ (0, D.jsx)("h2", {
					id: "queue-next-heading",
					children: "Pending messages"
				}), /* @__PURE__ */ (0, D.jsxs)("span", {
					"data-queue-count": "",
					children: [
						a?.items.length || 0,
						" / ",
						8
					]
				})]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine queue-next-boundary",
				children: "This bounded queue belongs to the current live run. Stopped or uncertain items are review-only, not scheduled. Closing or restarting does not automatically resume them. Remove reviewed items before starting or switching conversations; Copy to draft never sends."
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				"data-queue-error": "",
				className: "error",
				role: "alert",
				hidden: !i.error && !s,
				children: s ? "Could not verify the pending queue. Controls are disabled; nothing will be retried." : i.error || ""
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				"data-queue-unknown": "",
				hidden: !i.unknown,
				children: [/* @__PURE__ */ (0, D.jsx)("p", {
					className: "fine",
					children: "A queue request’s outcome is unknown. The message may already be queued or delivered. Your draft is kept; nothing will retry."
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-queue-reviewed": "",
					disabled: !i.unknown || !i.reviewable || !o?.connected || !!i.busy,
					onClick: () => r("reviewed"),
					children: "I’ve reviewed the queue and conversation"
				})]
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				"data-queue-copy-notice": "",
				hidden: !i.copiedDraft,
				children: [/* @__PURE__ */ (0, D.jsx)("span", {
					className: "fine",
					children: "Copied to draft only. Your earlier draft is kept."
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-queue-restore-draft": "",
					disabled: !!i.busy || !!o?.editing,
					onClick: () => r("restore"),
					children: "Restore earlier draft"
				})]
			}),
			/* @__PURE__ */ (0, D.jsx)("div", {
				"data-queue-items": "",
				className: "queue-next-items",
				children: c.map((t) => /* @__PURE__ */ (0, D.jsx)(Ii, {
					...e,
					item: t
				}, t.id))
			})
		]
	});
}
//#endregion
//#region src/queue/controller.tsx
var Ri = /* @__PURE__ */ new Map(), L = null;
function zi(e) {
	Bi();
	let t = e.root.querySelector("[data-react-composer-queue]");
	if (!t || e.root.dataset.queueNextEnabled !== "true") return;
	let n = JSON.stringify([
		e.root.dataset.project,
		e.root.dataset.session,
		e.root.dataset.instance
	]), r = Ri.get(n) || {
		editors: /* @__PURE__ */ new Map(),
		unknown: !1
	};
	for (Ri.delete(n), Ri.set(n, r); Ri.size > 16;) Ri.delete(Ri.keys().next().value);
	let i = {
		api: e,
		root: (0, d.createRoot)(t),
		container: t,
		controller: new AbortController(),
		store: r,
		queue: null,
		ui: null,
		invalid: !1,
		composing: !1,
		snapshotRevision: -1,
		composeRevision: 0,
		panelRef: (0, l.createRef)(),
		rowRefs: /* @__PURE__ */ new Map()
	};
	L = i;
	let a = { signal: i.controller.signal };
	e.root.addEventListener("click", (e) => {
		L === i && e.target instanceof Element && e.target.closest("button")?.matches("[data-queue-next]") && qi();
	}, a);
	for (let t of ["compositionstart", "compositionend"]) e.root.addEventListener(t, (n) => {
		L === i && n.target instanceof Element && n.target.id === "live-prompt" && (i.composing = t === "compositionstart", i.composeRevision++, e.changed());
	}, a);
	Wi(i);
}
function Bi() {
	if (!L) return;
	let e = L;
	L = null, e.controller.abort(), e.store.busy && (e.store.busy = !1, e.store.unknown = !0, e.store.reviewable = !1), (0, u.flushSync)(() => e.root.unmount());
}
function Vi() {
	return !!L && Mi(L);
}
function Hi() {
	return !!L && ji(L);
}
function Ui(e, t, n = !0) {
	if (!L) return null;
	if (L.ui = t, e && Number.isSafeInteger(e.revision) && e.revision >= L.snapshotRevision) {
		let n = e.queue;
		n == null ? (L.queue = null, L.invalid = !1, L.lastRaw = null) : n !== L.lastRaw && (L.lastRaw = n, Di(n, L.api.validText) && (!L.queue || n.token !== L.queue.token || n.revision >= L.queue.revision) ? (L.queue = n, L.invalid = !1) : L.invalid = !0), L.snapshotRevision = e.revision, L.store.unknown && !L.store.busy && t.connected && (L.store.unknownRevision == null || e.revision > L.store.unknownRevision) && (L.store.reviewable = !0);
	}
	return n && Wi(L), Ni(L);
}
function Wi(e) {
	if (L !== e) return;
	let t = document.activeElement, n = !!t && e.container.contains(t);
	(0, u.flushSync)(() => e.root.render(/* @__PURE__ */ (0, D.jsx)(Li, {
		current: e,
		panelRef: e.panelRef,
		rowRefs: e.rowRefs,
		onAction: (t, n) => {
			L === e && Gi(e, t, n);
		},
		onText: (t, n) => {
			if (L !== e) return;
			let r = e.store.editors.get(t);
			r && (r.text = n, r.revision++, Wi(e));
		},
		onComposition: (t, n) => {
			if (L !== e) return;
			let r = e.store.editors.get(t);
			r && (r.composing = n, r.revision++, e.api.changed());
		}
	})));
	for (let [t, n] of e.rowRefs) !n.input && !n.edit && e.rowRefs.delete(t);
	n && !t?.isConnected && document.activeElement === document.body && !e.panelRef.current?.hidden && e.panelRef.current?.focus({ preventScroll: !0 });
}
function Gi(e, t, n = "") {
	let { store: r, api: i, queue: a, ui: o } = e;
	if (t === "reviewed") {
		if (r.unknown && r.reviewable && o?.connected && !r.busy) {
			r.unknown = !1, r.error = "";
			for (let e of r.editors.values()) e.token === a?.token && (e.baseRevision = a.revision);
			i.changed();
		}
		return;
	}
	if (t === "restore") {
		r.copiedDraft && !r.busy && !o?.editing && (i.writeDraft(r.copiedDraft.value, r.copiedDraft), r.copiedDraft = null, i.changed());
		return;
	}
	let s = a?.items.find((e) => e.id === n), c = r.editors.get(n);
	if (t === "edit" && Ai(e) && s?.state === "pending" && o?.status === "running" && a) r.editors.set(n, {
		text: s.text,
		revision: 0,
		baseRevision: a.revision,
		token: a.token
	}), Wi(e), e.rowRefs.get(n)?.input?.focus();
	else if (t === "cancel" && (!r.busy || r.busy.id !== n)) r.editors.delete(n), Wi(e), e.rowRefs.get(n)?.edit?.focus();
	else if (t === "remove") Ki("queue-remove", n);
	else if (t === "save" && c) Ki("queue-update", n, c.text);
	else if (t === "copy" && !r.busy && !o?.editing && (s ? ["held", "uncertain"].includes(s.state) : c)) {
		let e = c?.text ?? s?.text;
		if (typeof e != "string") return;
		r.copiedDraft ||= i.draft(), i.writeDraft(e), i.changed();
	}
}
async function Ki(e, t, n) {
	if (!L || !Ai(L) || e === "queue-enqueue" && !ji(L)) return !1;
	let r = L, { api: i, store: a, queue: o } = r;
	if (!o) return !1;
	let s = o.items.find((e) => e.id === t);
	if (e !== "queue-enqueue" && (!s || !(s.state === "pending" && r.ui?.status === "running" || e === "queue-remove" && ["held", "uncertain"].includes(s.state)))) return !1;
	if (e !== "queue-remove" && !i.validText(n)) return a.error = "Enter a nonblank message of at most 64 KiB, without null or malformed Unicode characters.", i.changed(), !1;
	let c = a.editors.get(t), l = c?.revision;
	if (e === "queue-remove" && c || e === "queue-update" && c?.composing) return !1;
	let u = e === "queue-update" ? c?.baseRevision : o.revision, d = e === "queue-update" ? c?.token : o.token;
	if (u == null || !Number.isSafeInteger(u) || d !== o.token) return !1;
	let f = document.activeElement instanceof HTMLElement && r.container.contains(document.activeElement) ? document.activeElement : null, p = {
		action: e,
		id: t,
		focus: f,
		revision: u,
		token: d
	};
	a.busy = p, a.error = "", a.reviewable = !1, i.changed();
	try {
		let o = {
			session_id: i.session,
			queue_token: d,
			queue_revision: String(u)
		};
		t && (o.item_id = t), e !== "queue-remove" && (o.text = n);
		let s = await i.request(e, o);
		if (L !== r || a.busy !== p) return !1;
		if (!Pi(s.queue, p, r.queue, i.validText)) throw Error("Unverified queue acknowledgement");
		return e === "queue-update" && c && (r.rowRefs.get(t)?.input !== document.activeElement && !c.composing && c.revision === l && c.text === n ? a.editors.delete(t) : c.baseRevision = s.queue.revision), e === "queue-remove" && a.editors.delete(t), p.verified = !0, !0;
	} catch {
		return L !== r || a.busy !== p ? !1 : (a.unknown = !0, a.unknownRevision = r.snapshotRevision, a.error = "Queue request outcome needs review. Your text is kept. Nothing will be retried automatically.", c && (c.error = "Your edit is kept. Review the current queue, then explicitly Save again if appropriate."), !1);
	} finally {
		if (L === r && a.busy === p && (a.busy = !1, i.changed(), p.verified && f?.isConnected && document.activeElement === document.body && f.getClientRects().length && !f.matches(":disabled") && !f.closest("[inert]") && f.focus({ preventScroll: !0 }), a.unknown)) {
			try {
				await i.request(), L === r && r.ui?.connected && !r.invalid && (a.reviewable = !0);
			} catch {}
			L === r && i.changed();
		}
	}
}
async function qi() {
	if (!L || !ji(L)) return;
	let e = L, t = e.api.draft(), n = e.composeRevision;
	if (await Ki("queue-enqueue", "", t.value)) {
		if (L !== e) return;
		!e.composing && e.composeRevision === n && Oi(t, e.api.draft()) && e.api.writeDraft(""), e.api.changed();
	}
}
var Ji = Object.freeze({
	init: zi,
	dispose: Bi,
	render: Ui,
	blocking: Vi,
	canEnqueue: Hi,
	enqueue: qi
});
//#endregion
//#region src/composer-context/Panel.tsx
function Yi({ kind: e }) {
	return /* @__PURE__ */ (0, D.jsx)("span", {
		className: "composer-mention-icon",
		"aria-hidden": "true",
		"data-kind": e,
		children: /* @__PURE__ */ (0, D.jsx)("svg", {
			viewBox: "0 0 24 24",
			width: "16",
			height: "16",
			fill: "none",
			stroke: "currentColor",
			strokeWidth: "1.6",
			strokeLinecap: "round",
			strokeLinejoin: "round",
			"aria-hidden": "true",
			focusable: "false",
			children: e === "skill" ? /* @__PURE__ */ (0, D.jsx)("path", { d: "m9 5 2 5 5 2-5 2-2 5-2-5-5-2 5-2 2-5Z M19 2l1 3 3 1-3 1-1 3-1-3-3-1 3-1 1-3Z" }) : e === "command" ? /* @__PURE__ */ (0, D.jsx)("path", { d: "m5 7 4 5-4 5M11 17h8" }) : e === "folder" ? /* @__PURE__ */ (0, D.jsx)("path", { d: "M3 6a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z" }) : /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("rect", {
				x: "5",
				y: "3",
				width: "14",
				height: "18",
				rx: "3"
			}), /* @__PURE__ */ (0, D.jsx)("path", { d: "M8 8h8M8 12h6" })] })
		})
	});
}
function Xi({ state: e, actions: t, refs: n }) {
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsx)("div", {
			className: "composer-context-items",
			"data-composer-context-items": !0,
			hidden: !e.items.length,
			children: e.items.map((n) => /* @__PURE__ */ (0, D.jsxs)("div", {
				className: `composer-context-chip${n.state === "error" ? " is-error" : ""}`,
				children: [
					n.url && /* @__PURE__ */ (0, D.jsx)("span", {
						className: "composer-context-thumbnail",
						children: /* @__PURE__ */ (0, D.jsx)("img", {
							src: n.url,
							alt: "",
							width: 32,
							height: 32,
							onError: (e) => t.previewError(n, n.url, e.currentTarget)
						})
					}),
					/* @__PURE__ */ (0, D.jsx)("span", {
						className: "composer-context-label",
						title: n.label,
						children: n.label
					}),
					n.state !== "ready" && /* @__PURE__ */ (0, D.jsx)("span", {
						className: "composer-context-detail",
						children: n.state === "pending" ? "Reading…" : n.error
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "composer-context-remove",
						"aria-label": `Remove attachment ${n.label}`,
						disabled: !e.canRemove,
						"data-context-item-id": n.id,
						onClick: () => t.remove(n),
						children: "×"
					})
				]
			}, n.id))
		}),
		/* @__PURE__ */ (0, D.jsx)("div", {
			className: `composer-context-status${e.error ? " is-error" : ""}`,
			"data-composer-context-status": !0,
			role: "status",
			"aria-live": "polite",
			hidden: !e.notice,
			title: e.privacy,
			children: e.notice
		}),
		/* @__PURE__ */ (0, D.jsxs)("div", {
			id: "composer-mentions",
			className: "composer-mentions",
			"data-composer-mentions": !0,
			role: "listbox",
			"aria-label": "Commands, files, folders, and installed skills",
			hidden: !e.popupVisible,
			ref: n.popup,
			style: e.popupStyle,
			children: [e.message && /* @__PURE__ */ (0, D.jsx)("div", {
				className: e.heading ? "composer-mention-heading" : "composer-mention-note",
				children: e.message
			}), e.rows.map((n, r) => /* @__PURE__ */ (0, D.jsxs)("div", {
				id: n.id,
				className: "composer-mention-option",
				role: "option",
				"aria-selected": r === e.selected,
				"aria-disabled": n.disabled || void 0,
				"data-composer-skills-retry": n.retrySkills ? "" : void 0,
				title: [n.title || n.label, n.fullDescription ?? n.description].filter(Boolean).join(" — "),
				onMouseDown: (e) => e.preventDefault(),
				onClick: () => t.choose(n),
				children: [
					/* @__PURE__ */ (0, D.jsx)(Yi, { kind: n.folder ? "folder" : e.marker === "$" ? "skill" : e.marker === "/" ? "command" : "file" }),
					/* @__PURE__ */ (0, D.jsxs)("span", {
						className: "composer-mention-main",
						children: [/* @__PURE__ */ (0, D.jsx)("span", {
							className: "composer-mention-name",
							title: n.label,
							children: n.label
						}), n.description && /* @__PURE__ */ (0, D.jsx)("span", {
							className: "composer-mention-description",
							title: n.fullDescription ?? n.description,
							children: n.description
						})]
					}),
					n.folder && /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("span", {
						className: "composer-mention-folder-hint",
						hidden: r !== e.selected,
						children: "Browse folder"
					}), /* @__PURE__ */ (0, D.jsx)("span", {
						className: "composer-mention-chevron",
						"aria-hidden": "true",
						children: "›"
					})] })
				]
			}, n.id))]
		}),
		e.menuOpen && /* @__PURE__ */ (0, D.jsx)("div", {
			id: "composer-context-menu",
			ref: n.menu,
			className: "snow-menu composer-context-menu",
			role: "menu",
			"aria-label": "Add context",
			tabIndex: -1,
			style: e.menuStyle,
			onKeyDown: t.menuKey,
			children: /* @__PURE__ */ (0, D.jsxs)("div", {
				className: "snow-menu-content",
				tabIndex: -1,
				children: [/* @__PURE__ */ (0, D.jsxs)("button", {
					type: "button",
					role: "menuitem",
					className: "snow-menu-row composer-context-menu-item",
					"data-composer-files": !0,
					disabled: !e.canFiles,
					onClick: () => t.marker("@"),
					children: [
						/* @__PURE__ */ (0, D.jsx)(Yi, { kind: "folder" }),
						/* @__PURE__ */ (0, D.jsx)("span", {
							className: "snow-menu-row-label",
							children: "Project files"
						}),
						/* @__PURE__ */ (0, D.jsx)("span", {
							className: "snow-menu-row-value",
							"aria-hidden": "true",
							children: "@"
						})
					]
				}), /* @__PURE__ */ (0, D.jsxs)("button", {
					type: "button",
					role: "menuitem",
					className: "snow-menu-row composer-context-menu-item",
					"data-composer-skills": !0,
					disabled: !e.canRead,
					onClick: () => t.marker("$"),
					children: [
						/* @__PURE__ */ (0, D.jsx)(Yi, { kind: "skill" }),
						/* @__PURE__ */ (0, D.jsx)("span", {
							className: "snow-menu-row-label",
							children: "Installed skills"
						}),
						/* @__PURE__ */ (0, D.jsx)("span", {
							className: "snow-menu-row-value",
							"aria-hidden": "true",
							children: "$"
						})
					]
				})]
			})
		})
	] });
}
function Zi({ state: e, actions: t, refs: n }) {
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("input", {
		type: "file",
		"data-composer-file-input": !0,
		multiple: !0,
		hidden: !0,
		disabled: !e.canAttach,
		ref: n.input,
		onChange: (e) => {
			e.currentTarget.files && t.addFiles(e.currentTarget.files), e.currentTarget.value = "";
		}
	}), /* @__PURE__ */ (0, D.jsxs)("div", {
		className: "composer-context-tools",
		"aria-label": "Add context",
		children: [/* @__PURE__ */ (0, D.jsx)("button", {
			type: "button",
			className: "composer-context-button",
			"data-composer-context-menu": !0,
			"aria-label": "Add context",
			title: "Add context",
			"aria-haspopup": "menu",
			"aria-expanded": e.menuOpen,
			"aria-controls": e.menuOpen ? "composer-context-menu" : void 0,
			disabled: !e.canRead,
			ref: n.trigger,
			onClick: t.menu,
			children: /* @__PURE__ */ (0, D.jsx)("svg", {
				className: "icon",
				width: "18",
				height: "18",
				viewBox: "0 0 24 24",
				fill: "none",
				stroke: "currentColor",
				strokeWidth: "1.7",
				strokeLinecap: "round",
				strokeLinejoin: "round",
				"aria-hidden": "true",
				children: /* @__PURE__ */ (0, D.jsx)("path", { d: "M12 5v14M5 12h14" })
			})
		}), /* @__PURE__ */ (0, D.jsx)("button", {
			type: "button",
			className: "composer-context-button",
			"data-composer-attach": !0,
			"aria-label": "Attach files",
			title: "Attach images or UTF-8 text files",
			disabled: !e.canAttach,
			ref: n.attach,
			onClick: t.attach,
			children: /* @__PURE__ */ (0, D.jsx)("svg", {
				className: "icon",
				width: "18",
				height: "18",
				viewBox: "0 0 24 24",
				fill: "none",
				stroke: "currentColor",
				strokeWidth: "1.6",
				strokeLinecap: "round",
				strokeLinejoin: "round",
				"aria-hidden": "true",
				children: /* @__PURE__ */ (0, D.jsx)("path", { d: "m8 12 6-6a3 3 0 0 1 4 4l-8 8a5 5 0 0 1-7-7l8-8m-5 13 8-8" })
			})
		})]
	})] });
}
var Qi = new TextEncoder(), $i = (e) => Qi.encode(e).length, ea = Object.freeze([
	{
		name: "/compact",
		description: "Compact older conversation context"
	},
	{
		name: "/context",
		description: "Open context and usage"
	},
	{
		name: "/default",
		description: "Switch to Default mode"
	},
	{
		name: "/goal",
		description: "Compose a persistent Thread Goal"
	},
	{
		name: "/model",
		description: "Choose a model"
	},
	{
		name: "/permissions",
		description: "Choose the session permission mode"
	},
	{
		name: "/plan",
		description: "Switch to Plan Mode"
	},
	{
		name: "/processes",
		description: "Open managed processes"
	},
	{
		name: "/sessions",
		description: "Open conversation actions"
	},
	{
		name: "/settings",
		description: "Open Web Manager settings"
	},
	{
		name: "/thinking",
		description: "Open thinking and response controls"
	},
	{
		name: "/tree",
		description: "Open conversation versions and branches"
	}
]);
function ta(e, t) {
	let n = 0;
	for (let r of e) r === t[n] && n++;
	return n === t.length;
}
function na(e) {
	let t = e.toLocaleLowerCase().replace(/^\//, ""), n = [], r = [], i = [];
	for (let e of ea) {
		let a = e.name.slice(1).toLocaleLowerCase();
		!t || a.startsWith(t) ? (a === t ? n : r).push(e) : t.length >= 3 && ta(a, t) && i.push(e);
	}
	return [
		...n,
		...r,
		...i
	];
}
function ra(e) {
	if (typeof e != "string" || e.includes("\0")) return !1;
	for (let t = 0; t < e.length; t++) {
		let n = e.charCodeAt(t);
		if (n >= 55296 && n <= 56319) {
			let n = e.charCodeAt(++t);
			if (!(n >= 56320 && n <= 57343)) return !1;
		} else if (n >= 56320 && n <= 57343) return !1;
	}
	return !0;
}
function ia(e) {
	let t = (t) => t.every((t, n) => e[n] === t);
	if (t([
		137,
		80,
		78,
		71,
		13,
		10,
		26,
		10
	])) return "image/png";
	if (t([
		255,
		216,
		255
	])) return "image/jpeg";
	let n = (t, n) => String.fromCharCode(...e.subarray(t, n));
	return ["GIF87a", "GIF89a"].includes(n(0, 6)) ? "image/gif" : n(0, 4) === "RIFF" && n(8, 12) === "WEBP" ? "image/webp" : "";
}
function aa(e, t) {
	let n = (t, n) => String.fromCharCode(...e.subarray(t, n)), r = (t) => e[t] * 256 + e[t + 1], i = (t) => e[t] + e[t + 1] * 256, a = (e) => r(e) * 65536 + r(e + 2), o = (t) => i(t) + e[t + 2] * 65536, s = (e) => i(e) + i(e + 2) * 65536, c = 0, l = 0;
	if (t === "image/png") {
		if (e.length < 33 || a(8) !== 13 || n(12, 16) !== "IHDR") return !1;
		c = a(16), l = a(20);
	} else if (t === "image/gif") {
		if (e.length < 13) return !1;
		c = i(6), l = i(8);
	} else if (t === "image/jpeg") {
		let t = 2;
		for (let n = 0; t < e.length && n < 1024; n++) {
			if (e[t++] !== 255) return !1;
			for (; t < e.length && e[t] === 255;) t++;
			let n = e[t++];
			if (n === 1 || n >= 208 && n <= 215) continue;
			if (!n || n === 216 || n === 217 || n === 218 || t + 2 > e.length) return !1;
			let i = r(t);
			if (i < 2 || t + i > e.length) return !1;
			if ([
				192,
				193,
				194
			].includes(n)) {
				if (i < 11 || e[t + 7] < 1 || e[t + 7] > 4 || i !== 8 + 3 * e[t + 7]) return !1;
				l = r(t + 3), c = r(t + 5);
				break;
			}
			t += i;
		}
	} else if (t === "image/webp") {
		if (e.length < 25 || s(4) + 8 !== e.length) return !1;
		let t = s(16), r = n(12, 16);
		if (t + t % 2 > e.length - 20) return !1;
		if (r === "VP8X" && t === 10) c = o(24) + 1, l = o(27) + 1;
		else if (r === "VP8L" && t >= 5 && e[20] === 47 && !(e[24] & 224)) {
			let e = s(21);
			c = (e & 16383) + 1, l = (e >>> 14 & 16383) + 1;
		} else r === "VP8 " && t >= 10 && !(e[20] & 1) && n(23, 26) === "*" && (c = i(26) & 16383, l = i(28) & 16383);
	}
	return c > 0 && l > 0 && c <= 16384 && l <= 16384 && c * l <= 4e7;
}
function oa(e) {
	let t = "";
	for (let n = 0; n < e.length; n += 8192) t += String.fromCharCode(...e.subarray(n, n + 8192));
	return btoa(t);
}
var R = (e) => `Attachment: ${e.label}\n${e.kind === "text" ? e.text : "[Image]"}`, sa = "Text and image contents will be sent to the provider and persisted in saved conversation history when you send. Images require a vision-capable model.";
function ca(e) {
	e.previewURL && URL.revokeObjectURL(e.previewURL), e.previewURL = null, e.previewElement = null;
}
function la(e) {
	if (e.state !== "ready" || e.kind !== "image" || e.previewFailed) return "";
	if (e.previewURL) return e.previewURL;
	try {
		if (e.size > 2097152 || ![
			"image/png",
			"image/jpeg",
			"image/gif",
			"image/webp"
		].includes(e.mime || "")) return "";
		let t = atob(e.data || ""), n = Uint8Array.from(t, (e) => e.charCodeAt(0));
		return n.length !== e.size || ia(n) !== e.mime || !aa(n, e.mime || "") ? (e.previewFailed = !0, "") : (e.previewURL = URL.createObjectURL(new Blob([n], { type: e.mime })), e.previewURL);
	} catch {
		return e.previewFailed = !0, "";
	}
}
//#endregion
//#region src/composer-context/controller.tsx
var ua = /* @__PURE__ */ new Map(), da = 0, fa = null;
function pa(e) {
	e.owner = null;
	for (let t of e.items) ca(t), t.previewFailed = !1, t.state === "pending" && (t.state = "error", t.error = "Read interrupted. Remove this attachment and add it again.", t.version++);
	e.revision++;
}
function ma(e) {
	let t = ua.get(e);
	t && pa(t), ua.delete(e);
}
function ha({ root: e, key: t, instance: n, request: r, changed: i = () => {}, error: a = () => {}, replaceText: o, focusPrompt: s = () => {}, suggestions: c = () => {}, command: f }) {
	fa?.dispose(), fa = null;
	let p = (t) => e.querySelector(t), m = e.querySelector("#live-prompt"), h = p("[data-composer-context-root]"), g = p("[data-composer-context-tools-root]");
	if (!m || !h || !g) throw Error("Composer context markup is incomplete");
	let _ = m, v = h, y = g, b = (0, d.createRoot)(v), x = {
		input: (0, l.createRef)(),
		attach: (0, l.createRef)(),
		trigger: (0, l.createRef)(),
		popup: (0, l.createRef)(),
		menu: (0, l.createRef)()
	}, S = ua.get(t) || {
		items: [],
		revision: 0
	};
	ua.has(t) && (pa(S), ua.delete(t));
	let ee = {};
	for (S.owner = ee, ua.set(t, S); ua.size > 16;) ma(ua.keys().next().value);
	let C = new AbortController(), w = { signal: C.signal }, te = !1, T = {
		safe: !1,
		editable: !1,
		readable: !1
	}, ne = 0, E = null, re = [], ie = 0, ae = null, oe = null, se = "", ce = !1, le = 0, ue = 0, de = 0, fe, pe = !1, me = !1, he = "", ge = !1, _e = {}, ve = {}, ye = "", be = () => !te && S.owner === ee && ua.get(t) === S, xe = () => be() && T.editable && !_.closest("[inert], [hidden]"), Se = () => xe() && T.safe, O = () => Se() && T.readable, k = () => be() && (le > 0 || de > 0 || S.items.some((e) => e.state === "pending")), Ce = () => be() && S.items.length > 0;
	function we() {
		ne++, E = null, re = [], me = !1, he = "", Oe();
	}
	function Te(e = !1) {
		pe && (pe = !1, Oe(), e && x.trigger.current?.focus({ preventScroll: !0 }));
	}
	function Ee() {
		if (O()) {
			if (pe) {
				Te(!0);
				return;
			}
			we(), pe = !0, Oe(), Ae(), x.menu.current?.querySelector("button:not(:disabled)")?.focus({ preventScroll: !0 });
		}
	}
	function De(e) {
		be() && (se = e, ke(), a(e), i());
	}
	function A() {
		be() && (S.revision++, ke(), i());
	}
	function Oe() {
		if (!be()) return;
		let t = S.items.some((e) => e.kind === "image") ? " Images need a vision-capable model." : "", n = [
			se,
			S.items.length ? `On Send: shared with provider and saved in chat.${t}` : "",
			!T.editable && S.items.length ? "Attachments are kept; finish or cancel the current operation to change them." : ""
		].filter(Boolean).join(" "), r = {
			items: S.items.map((e) => ({
				...e,
				url: la(e)
			})),
			canRemove: xe(),
			canAttach: Se() && S.items.length < 8,
			canRead: O(),
			canFiles: O() && S.items.length < 8,
			notice: n,
			privacy: S.items.length ? sa : "",
			error: !!se,
			popupVisible: me,
			rows: [...re],
			selected: ie,
			message: he,
			heading: ge,
			marker: E?.marker || "",
			menuOpen: pe,
			popupStyle: _e,
			menuStyle: ve
		}, i = {
			remove: (t) => {
				let n = S.items.find((e) => e.id === t.id);
				xe() && n && (ca(n), S.items = S.items.filter((e) => e !== n), se = "", A(), (!e.ownerDocument.activeElement || e.ownerDocument.activeElement === e.ownerDocument.body) && x.attach.current?.focus({ preventScroll: !0 }));
			},
			previewError: (e, t, n) => {
				let r = S.items.find((t) => t.id === e.id);
				be() && r && r.previewURL === t && n.getAttribute("src") === t && v.contains(n) && (ca(r), r.previewFailed = !0, ke());
			},
			choose: (e) => {
				E && Be(E) && re.includes(e) && !e.disabled && e.choose();
			},
			addFiles: Re,
			attach: () => {
				Se() && x.input.current?.click();
			},
			menu: Ee,
			marker: (e) => {
				!pe || !O() || e === "@" && S.items.length >= 8 || (Te(), $e(e));
			},
			menuKey: (t) => {
				if (!pe || t.nativeEvent.isComposing) return;
				if (t.key === "Escape" || t.key === "Tab") {
					t.key === "Escape" && (t.preventDefault(), t.stopPropagation()), Te(!0);
					return;
				}
				if (t.key === "PageDown" || t.key === "PageUp") {
					t.preventDefault();
					let e = x.menu.current?.querySelector(".snow-menu-content");
					e && (e.scrollTop += (t.key === "PageUp" ? -1 : 1) * e.clientHeight);
					return;
				}
				if (![
					"ArrowDown",
					"ArrowUp",
					"Home",
					"End"
				].includes(t.key)) return;
				t.preventDefault(), t.stopPropagation();
				let n = [...x.menu.current?.querySelectorAll("button:not(:disabled)") || []], r = n.indexOf(e.ownerDocument.activeElement), i = n[t.key === "Home" ? 0 : t.key === "End" || t.key === "ArrowUp" && r < 0 ? n.length - 1 : (r + (t.key === "ArrowUp" ? -1 : 1) + n.length) % n.length];
				i?.focus({ preventScroll: !0 });
				let a = x.menu.current?.querySelector(".snow-menu-content");
				if (i && a) {
					let e = a.getBoundingClientRect(), t = i.getBoundingClientRect();
					t.top < e.top ? a.scrollTop -= e.top - t.top : t.bottom > e.bottom && (a.scrollTop += t.bottom - e.bottom);
				}
			}
		}, a = e.ownerDocument.activeElement, o = a && v.contains(a) ? a.getAttribute("data-context-item-id") : null;
		if ((0, u.flushSync)(() => b.render(/* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)(Xi, {
			state: r,
			actions: i,
			refs: x
		}), (0, u.createPortal)(/* @__PURE__ */ (0, D.jsx)(Zi, {
			state: r,
			actions: i,
			refs: x
		}), y)] }))), o) {
			let t = [...v.querySelectorAll("[data-context-item-id]")].find((e) => e.dataset.contextItemId === o) || x.attach.current;
			t && t !== e.ownerDocument.activeElement && t.focus({ preventScroll: !0 });
		}
		let s = {
			controls: "composer-mentions",
			expanded: me,
			activeDescendant: me ? re[ie]?.id : void 0
		}, l = JSON.stringify(s);
		l !== ye && (ye = l, c(s));
	}
	function ke() {
		be() && (Oe(), Ue(), Ae());
	}
	function Ae() {
		let e = x.menu.current, t = x.trigger.current;
		if (!pe || !e || !t) return;
		let n = window.visualViewport, r = t.getBoundingClientRect(), i = n?.offsetLeft || 0, a = n?.offsetTop || 0, o = n?.width || window.innerWidth, s = n?.height || window.innerHeight;
		ve = {
			minWidth: Math.max(0, Math.min(240, o - 24)),
			maxWidth: Math.max(0, Math.min(420, o - 24)),
			maxHeight: Math.max(0, Math.min(360, s - 96)),
			left: Math.max(i + 12, Math.min(r.left, i + o - e.offsetWidth - 12)),
			top: Math.max(a + 12, Math.min(r.top - 8 - e.offsetHeight, a + s - e.offsetHeight - 12))
		}, Oe();
	}
	function je(e) {
		T = {
			...T,
			...e
		}, O() || (we(), Te()), ke();
	}
	function Me(e, t = 0) {
		if (!Se()) return null;
		if (S.items.length >= 8) return De("At most 8 attachments can be added. Remove one before adding another."), null;
		if (!ra(e) || $i(e) > 4096) return De("Attachment name is invalid or too long."), null;
		if (!Number.isSafeInteger(t) || t < 0 || t > 2097152 || ue + t > 2097152 || le >= 8) return De("Attachments being read must total at most 2 MiB. Wait for current reads or choose a smaller file."), null;
		let n = {
			id: ++da,
			version: 0,
			label: e,
			state: "pending",
			size: t
		};
		return S.items.push(n), le++, ue += t, se = "", A(), n;
	}
	let Ne = (e) => be() && S.items.includes(e) && e.state === "pending";
	function Pe(e, t) {
		if (!Ne(e)) return !1;
		let n = {
			...e,
			...t,
			state: "ready"
		}, r = S.items.filter((t) => t !== e && t.state === "ready");
		if (r.reduce((e, t) => e + $i(R(t)), $i(R(n))) > 65536) throw Error("Attachment text and labels must total at most 64 KiB.");
		if (r.reduce((e, t) => e + (t.kind === "image" ? t.size : 0), n.kind === "image" ? n.size : 0) > 2097152) throw Error("Image attachments must total at most 2 MiB.");
		return Object.assign(e, n), e.version++, A(), !0;
	}
	function Fe(e, t) {
		Ne(e) && (e.state = "error", e.error = t, e.version++, A());
	}
	function Ie(e) {
		le--, ue -= e, be() && (ke(), i());
	}
	async function Le(e) {
		let t = Me(e.name || "Pasted image", e.size);
		if (t) try {
			let n = new Uint8Array(await e.arrayBuffer());
			if (!Ne(t)) return;
			if (n.length !== e.size || n.length > 2097152) throw Error("File changed or exceeds the attachment size limit.");
			let r = ia(n);
			if (r) Pe(t, {
				kind: "image",
				mime: r,
				data: oa(n),
				size: n.length
			});
			else {
				if (/^image\//i.test(e.type || "") || String.fromCharCode(...n.subarray(0, 5)) === "%PDF-") throw Error("Only PNG, JPEG, GIF, WebP images or UTF-8 text can be attached; PDFs are not supported.");
				if (n.length > 65536) throw Error("Text attachments must total at most 64 KiB.");
				let r;
				try {
					r = new TextDecoder("utf-8", { fatal: !0 }).decode(n);
				} catch {
					throw Error("This file is not valid UTF-8 text or a supported image.");
				}
				if (!ra(r)) throw Error("Text attachments cannot contain NUL bytes or invalid Unicode.");
				Pe(t, {
					kind: "text",
					text: r,
					size: n.length
				});
			}
		} catch (e) {
			Fe(t, (e instanceof Error ? e.message : "") || "Unable to read attachment. Remove it and try again.");
		} finally {
			Ie(e.size);
		}
	}
	function Re(e) {
		if (Se()) {
			for (let t = 0; t < Math.min(e.length, 8); t++) Le(e[t]);
			e.length > 8 && De("Only the first 8 files were considered. At most 8 attachments are allowed.");
		}
	}
	function ze() {
		if (_.selectionStart !== _.selectionEnd) return null;
		let e = _.selectionStart, t = _.value.slice(0, e), n = /^\/([^\s/]*)$/.exec(t);
		if (n && !_.value.slice(e).trim()) return {
			marker: "/",
			value: n[1],
			revision: 0,
			path: ".",
			filter: "",
			start: 0,
			end: e,
			text: _.value,
			caret: e
		};
		let r = /(?:^|\s)([@$])(?:"([^"\n]*)|([^\s"@$]*))$/.exec(t);
		if (!r) return null;
		let i = r[1], a = r[2] ?? r[3], o = a;
		if (r[2] !== void 0) try {
			o = JSON.parse(`"${a}"`);
		} catch {
			return null;
		}
		return {
			marker: i,
			value: o,
			revision: 0,
			path: ".",
			filter: "",
			start: e - a.length - 1 - (r[2] === void 0 ? 0 : 1),
			end: e,
			text: _.value,
			caret: e
		};
	}
	let Be = (e) => !!e && be() && O() && E === e && e.revision === ne && _.value === e.text && _.selectionStart === e.caret && _.selectionEnd === e.caret;
	function Ve(e, t, n = !1) {
		return !Be(e) || !o?.(t, e.start, e.end) ? !1 : (we(), Qe(), s(), n || we(), !0);
	}
	function He(e) {
		ie = e, Oe();
		let t = x.popup.current, n = t?.querySelectorAll("[role=\"option\"]")[ie];
		n && t && (n.offsetTop < t.scrollTop ? t.scrollTop = n.offsetTop : n.offsetTop + n.offsetHeight > t.scrollTop + t.clientHeight && (t.scrollTop = n.offsetTop + n.offsetHeight - t.clientHeight));
	}
	function Ue() {
		let t = x.popup.current;
		if (!me || !t) return;
		let n = (p("#live-composer") || e).getBoundingClientRect(), r = _.getBoundingClientRect(), i = window.visualViewport, a = (i?.offsetLeft || 0) + 8, o = (i?.offsetTop || 0) + 8, s = Math.max(0, (i?.width || window.innerWidth) - 16), c = Math.max(0, (i?.height || window.innerHeight) - 16), l = o + c, u = Math.min(120, c), d = n.top - 8 - o, f = r.top - 8 - o, m = l - r.bottom - 8, h, g, v = !1;
		d >= u ? (h = d, g = n.top - 8) : f >= u ? (h = f, g = r.top - 8) : m >= u ? (h = m, g = r.bottom + 8, v = !0) : (h = c, g = l);
		let y = Math.min(Math.max(0, n.width), s);
		_e = {
			..._e,
			width: `${y}px`,
			maxHeight: `${Math.min(320, Math.max(0, h))}px`,
			left: `${Math.max(a, Math.min(n.left, a + s - y))}px`
		}, Oe();
		let b = Math.min(t.offsetHeight, h, 320);
		_e = {
			..._e,
			top: `${Math.max(o, Math.min(v ? g : g - b, l - b))}px`
		}, Oe(), He(ie);
	}
	function We(e, t, n = "", r = !1) {
		Be(e) && (re = t.map((e) => ({
			...e,
			id: `composer-context-option-${++da}`
		})), ie = 0, me = !0, he = n, ge = r, Oe(), He(0), Ue());
	}
	function Ge(e) {
		return typeof e == "string" && $i(e) <= 4096 && ra(e) && !/[\x00-\x1f\x7f\\:]/.test(e) && e.split("/").every((e) => e && e !== "." && e !== "..");
	}
	function Ke(e) {
		let t = na(e.value).map((t) => ({
			label: t.name,
			description: t.description,
			command: t.name,
			choose: () => {
				Be(e) && Ve(e, "") && (f?.(t.name) || De(`${t.name} is unavailable in this Web Manager state.`));
			}
		}));
		We(e, t, t.length ? "Web Manager commands" : "No matching Web Manager commands.", t.length > 0);
	}
	function qe(e, t) {
		let n = e.filter.toLocaleLowerCase();
		return t.entries.filter((e) => e.name.toLocaleLowerCase().includes(n));
	}
	function Je(e, t) {
		if (!Be(e)) return;
		let n = qe(e, t).map((t) => ({
			label: t.name,
			title: t.path,
			folder: t.kind === "directory",
			choose: () => {
				t.kind === "directory" ? Ve(e, `@${JSON.stringify(t.path + "/").replaceAll("$", "\\u0024").slice(0, -1)}`, !0) : Xe(e, t.path);
			}
		}));
		t.hasMore && t.offset < 4096 && t.entries.length < 4096 && n.push({
			label: "More files…",
			choose: () => Ye(e, t.offset, t)
		}), We(e, n, t.limited ? "Directory listing is limited to 4,096 entries. Type a folder path to narrow it." : n.length ? "Files & folders" : "No matching files in this folder.", !t.limited && n.length > 0);
	}
	async function Ye(e, t = 0, n = null) {
		if (Be(e) && !e.loading) {
			e.loading = !0, de++, i(), We(e, [], e.filter ? "Searching this folder…" : "Loading files…");
			try {
				let i = n, a = t;
				for (;;) {
					let t = await r("files", {
						path: e.path,
						offset: a
					});
					if (!Be(e)) return;
					if (!t || t.path !== e.path || !Array.isArray(t.entries) || t.entries.length > 256) throw Error("Invalid directory listing.");
					let n = t.entries.filter((t) => t && ["file", "directory"].includes(t.kind) && Ge(t.path) && typeof t.name == "string" && t.name === t.path.split("/").at(-1) && !t.path.includes("\"") && (e.path === "." ? !t.path.includes("/") : t.path.slice(0, t.path.lastIndexOf("/")) === e.path)), o = [...i?.entries || [], ...n].slice(0, 4096);
					if (i = {
						path: e.path,
						entries: o,
						offset: t.next_offset,
						hasMore: t.has_more === !0 && Number.isSafeInteger(t.next_offset) && t.next_offset > a,
						limited: t.limited === !0 || o.length >= 4096
					}, ae = i, !e.filter || qe(e, i).length || !i.hasMore || i.entries.length >= 4096) break;
					a = i.offset;
				}
				i && Je(e, i);
			} catch {
				Be(e) && We(e, [], "Could not list this folder. Edit the @path to try again.");
			} finally {
				e.loading = !1, de--, be() && i();
			}
		}
	}
	async function Xe(e, t) {
		if (!Be(e) || e.reading) return;
		let n = Me(t);
		if (n) {
			e.reading = !0, We(e, [], "Reading selected file…");
			try {
				let i = await r("file", { path: t });
				if (!Ne(n)) return;
				if (!Be(e)) throw Error("Selection changed during the read. Remove this attachment and select the file again.");
				if (!i || i.path !== t || i.truncated !== !1) throw Error("This file preview is truncated or unavailable. Partial files are not attached.");
				if (!ra(i.text) || !Number.isSafeInteger(i.size) || i.size < 0 || i.size > 65536 || $i(i.text) > 65536) throw Error("Selected file must be UTF-8 text without NUL bytes, at most 64 KiB.");
				Pe(n, {
					kind: "text",
					text: i.text,
					size: i.size
				}) && Ve(e, `@${JSON.stringify(t).replaceAll("$", "\\u0024")} `);
			} catch (t) {
				Fe(n, (t instanceof Error ? t.message : "") || "Could not read selected file."), Be(e) && we();
			} finally {
				e.reading = !1, Ie(0);
			}
		}
	}
	async function Ze(e) {
		if (Be(e) && !e.skillsLoading) {
			e.skillsLoading = !0, de++, i(), We(e, [], "Loading installed skills…");
			try {
				oe || (S.skillCatalog?.instance === n ? oe = S.skillCatalog.promise : (oe = Promise.resolve().then(() => r("skills", {})), S.skillCatalog = {
					instance: n,
					promise: oe
				}));
				let t = await oe;
				if (!Be(e)) return;
				if (!t || t.instance_id !== n || !Array.isArray(t.skills) || t.skills.length > 4096) throw Error("Invalid installed skill catalog.");
				if (t.enabled !== !0) {
					We(e, [], "Close this runtime, enable installed skills in Settings → Workspaces, then start again.");
					return;
				}
				let i = t.skills.filter((t) => t && typeof t.name == "string" && /^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$/.test(t.name) && t.name.toLowerCase().includes(e.value.toLowerCase())).slice(0, 256).map((t) => ({
					label: `$${t.name}`,
					description: t.enabled === !0 ? String(t.description || "").slice(0, 512) : `Disabled${typeof t.disabled_by == "string" ? `: ${t.disabled_by.slice(0, 128)}` : ""}`,
					fullDescription: t.enabled === !0 ? String(t.description || "") : `Disabled${typeof t.disabled_by == "string" ? `: ${t.disabled_by}` : ""}`,
					disabled: t.enabled !== !0,
					choose: () => Ve(e, `$${t.name} `)
				}));
				We(e, i, t.limited || i.length === 256 ? "Catalog is limited. Type a skill name to narrow results." : i.length ? "Skills" : "No matching enabled installed skills.", !t.limited && i.length > 0 && i.length < 256);
			} catch {
				Be(e) && We(e, [{
					label: "Retry loading skills",
					retrySkills: !0,
					choose: () => {
						Be(e) && !e.skillsLoading && (S.skillCatalog?.promise === oe && delete S.skillCatalog, oe = null, Ze(e));
					}
				}], "Installed skills could not be loaded for this runtime. Nothing was activated. Retry only when you choose.");
			} finally {
				e.skillsLoading = !1, de--, be() && i();
			}
		}
	}
	function Qe() {
		if (we(), !O() || ce) return;
		let e = ze();
		if (!e) return;
		if (e.revision = ne, E = e, e.marker === "/") {
			Ke(e);
			return;
		}
		if (e.marker === "$") {
			Ze(e);
			return;
		}
		let t = e.value.lastIndexOf("/");
		if (e.path = t < 0 ? "." : e.value.slice(0, t), e.filter = e.value.slice(t + 1), e.path !== "." && !Ge(e.path)) {
			we();
			return;
		}
		if (ae?.path !== e.path) {
			Ye(e);
			return;
		}
		if (e.filter && !qe(e, ae).length && ae.hasMore && ae.offset < 4096 && ae.entries.length < 4096) {
			Ye(e, ae.offset, ae);
			return;
		}
		Je(e, ae);
	}
	function $e(e) {
		if (!O()) return;
		let t = _.selectionStart;
		o?.(`${t && !/\s/.test(_.value[t - 1]) ? " " : ""}${e}`, t, _.selectionEnd) && (s(), Qe());
	}
	function et(e = _.value) {
		if (!be()) throw Error("This attachment draft is no longer active.");
		if (k()) throw Error("Wait for attachment reads to finish before sending.");
		if (S.items.some((e) => e.state === "error")) throw Error("Remove failed attachments before sending; they have not been silently omitted.");
		if (!ra(e)) throw Error("Prompt must be valid Unicode without NUL bytes.");
		let n = S.items.map((e) => Object.freeze({
			id: e.id,
			version: e.version
		})), r = e.trim() ? e : n.length ? "Please review the attached files." : e, i = [];
		for (let e of S.items) i.push({
			type: "text",
			text: R(e)
		}), e.kind === "image" && i.push({
			type: "image",
			mime_type: e.mime,
			data: e.data
		});
		if (i.reduce((e, t) => e + (t.type === "text" ? $i(t.text || "") : 0), $i(r)) > 131072) throw Error("Prompt and attachment text must total at most 128 KiB, including labels.");
		return Object.freeze({
			revision: S.revision,
			items: Object.freeze(n),
			content: JSON.stringify(i),
			text: r,
			hasContent: n.length > 0,
			key: t,
			owner: ee
		});
	}
	function tt(e) {
		be() && e?.key === t && e.owner === ee && (S.items = S.items.filter((t) => !e.items.some((e) => e.id === t.id && e.version === t.version) || (ca(t), !1)), se = "", A());
	}
	let nt = () => {
		Ue(), Ae();
	};
	window.addEventListener?.("resize", nt, w), window.addEventListener?.("scroll", nt, {
		...w,
		capture: !0
	}), window.visualViewport?.addEventListener("resize", nt, w), window.visualViewport?.addEventListener("scroll", nt, w), window.ResizeObserver && (fe = new ResizeObserver(nt), fe.observe(p("#live-composer") || e)), _.addEventListener("input", Qe, w), _.addEventListener("compositionstart", () => {
		ce = !0, we();
	}, w), _.addEventListener("compositionend", () => {
		ce = !1, Qe();
	}, w), _.addEventListener("click", () => {
		E && !Be(E) && we();
	}, w), _.addEventListener("blur", we, w), _.addEventListener("keyup", () => {
		E && !Be(E) && we();
	}, w), _.addEventListener("keydown", (e) => {
		if (!(e.isComposing || ce || !me)) {
			if (!Be(E)) {
				we();
				return;
			}
			(!e.ctrlKey && !e.metaKey && !e.altKey && !e.shiftKey || e.key === "Enter" && E.marker === "/") && (e.key === "Escape" ? (e.preventDefault(), e.stopPropagation(), we()) : ["ArrowDown", "ArrowUp"].includes(e.key) && re.length ? (e.preventDefault(), e.stopPropagation(), He((ie + (e.key === "ArrowDown" ? 1 : -1) + re.length) % re.length)) : e.key === "Enter" && re.length ? (e.preventDefault(), e.stopPropagation(), re[ie].disabled || re[ie].choose()) : e.key === "Tab" && re.length && !re[ie].disabled ? (e.preventDefault(), e.stopPropagation(), re[ie].choose()) : e.key === "Tab" && we());
		}
	}, w);
	let rt = p("#live-composer") || e;
	rt.addEventListener("submit", (e) => {
		if (!_.value.startsWith("/")) return;
		e.preventDefault(), e.stopPropagation();
		let t = _.value.trim(), n = /^\/([^\s/]*)$/.exec(t), r = n?.[1] === void 0 ? void 0 : na(n[1]).find((e) => e.name.toLocaleLowerCase() === `/${n[1].toLocaleLowerCase()}`);
		if (r) {
			if (!o?.("", 0, _.value.length)) {
				De(`${r.name} is unavailable in this Web Manager state.`);
				return;
			}
			we(), s(), f?.(r.name) || De(`${r.name} is unavailable in this Web Manager state.`);
			return;
		}
		Qe(), E?.marker === "/" && Be(E) ? We(E, re, "Choose a Web Manager command before sending.") : De("Choose a Web Manager command before sending.");
	}, {
		...w,
		capture: !0
	}), rt.addEventListener("dragover", (e) => {
		[...e.dataTransfer?.types || []].includes("Files") && (e.preventDefault(), e.dataTransfer.dropEffect = Se() ? "copy" : "none");
	}, w), rt.addEventListener("drop", (e) => {
		e.dataTransfer?.files?.length && (e.preventDefault(), Re(e.dataTransfer.files));
	}, w), rt.addEventListener("paste", (e) => {
		let t = [...e.clipboardData?.items || []].filter((e) => e.kind === "file" && /^image\//.test(e.type)).slice(0, 8).map((e) => e.getAsFile()).filter((e) => !!e);
		t.length && (e.preventDefault(), Re(t));
	}, w);
	function it() {
		te || (we(), Te(), C.abort(), fe?.disconnect(), be() && pa(S), te = !0, (0, u.flushSync)(() => b.unmount()), c({ expanded: !1 }));
	}
	return e.ownerDocument.addEventListener("snow:navigation-before-swap", () => Te(!1), w), e.ownerDocument.addEventListener("pointerdown", (e) => {
		pe && e.target instanceof Node && !x.menu.current?.contains(e.target) && !x.trigger.current?.contains(e.target) && Te(!1);
	}, w), e.ownerDocument.addEventListener("focusin", (e) => {
		pe && e.target instanceof Node && !x.menu.current?.contains(e.target) && !x.trigger.current?.contains(e.target) && Te(!1);
	}, w), we(), ke(), fa = Object.freeze({
		render: je,
		capture: et,
		accepted: tt,
		hasAttachments: Ce,
		pending: k,
		dispose: it
	}), fa;
}
var ga = Object.freeze({
	init: ha,
	forget: ma,
	render: (e) => fa?.render(e),
	capture: (e) => {
		if (!fa) throw Error("Composer context is unavailable.");
		return fa.capture(e);
	},
	accepted: (e) => fa?.accepted(e),
	hasAttachments: () => fa?.hasAttachments() || !1,
	pending: () => fa?.pending() || !1,
	dispose: () => {
		fa?.dispose(), fa = null;
	}
});
//#endregion
//#region src/live-view/Chrome.tsx
function _a({ error: e }) {
	let [t, n] = (0, l.useState)(!1);
	function r(e) {
		let t = e.currentTarget.closest("#live-session")?.querySelector("#live-prompt");
		t && !t.disabled ? t.focus({ preventScroll: !0 }) : e.currentTarget.blur(), n(!0);
	}
	return /* @__PURE__ */ (0, D.jsxs)("div", {
		id: "live-error",
		className: "error live-error live-error-message",
		role: "alert",
		hidden: !e || t,
		children: [/* @__PURE__ */ (0, D.jsx)("span", { children: e }), /* @__PURE__ */ (0, D.jsx)("button", {
			type: "button",
			className: "quiet icon-button",
			"data-live-error-dismiss": "",
			"aria-label": "Dismiss error",
			title: "Dismiss error",
			onClick: r,
			children: "×"
		})]
	});
}
function va({ view: e }) {
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "connection-line",
			children: [/* @__PURE__ */ (0, D.jsx)("span", {
				id: "live-connection",
				role: "status",
				"data-connected": String(e.connected),
				children: e.connection
			}), /* @__PURE__ */ (0, D.jsx)("button", {
				className: "quiet",
				type: "button",
				"data-runtime-reload": "",
				hidden: !e.reload,
				children: "Review / reload workspace"
			})]
		}),
		/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "unknown-outcome",
			id: "live-unknown",
			role: "alert",
			hidden: !e.unknown,
			children: [/* @__PURE__ */ (0, D.jsx)("p", { children: "A request’s outcome is unknown. Nothing will be retried or replayed. Review the conversation and runtime state before sending anything again; the original request may have succeeded. Your draft is kept." }), /* @__PURE__ */ (0, D.jsx)("button", {
				type: "button",
				"data-runtime-reviewed": "",
				disabled: e.reviewDisabled,
				children: "I’ve reviewed the current conversation"
			})]
		}),
		/* @__PURE__ */ (0, D.jsxs)("aside", {
			id: "live-recovery",
			className: "notice recovery-notice",
			role: "status",
			hidden: !e.recoveryVisible,
			children: [/* @__PURE__ */ (0, D.jsx)("p", {
				id: "live-recovery-message",
				children: e.recoveryMessage
			}), /* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				children: "Tools may already have had effects. Close this runtime to review saved history, then explicitly resume if appropriate. Nothing will be replayed."
			})]
		}),
		/* @__PURE__ */ (0, D.jsx)(_a, { error: e.error }, e.error),
		/* @__PURE__ */ (0, D.jsx)("p", {
			id: "live-turn-outcome",
			className: "notice live-error",
			role: "status",
			hidden: !e.canceled,
			children: e.canceled ? "The last turn was canceled before completion. Nothing will retry automatically. You can send another message." : ""
		})
	] });
}
//#endregion
//#region src/live-view/Composer.tsx
function ya({ ref: e }) {
	let [t, n] = (0, l.useState)(""), [r, i] = (0, l.useState)(!1), [a, o] = (0, l.useState)({ expanded: !1 }), s = (0, l.useRef)(!1);
	return (0, l.useImperativeHandle)(e, () => ({
		update(e) {
			return !s.current && (n(e), !0);
		},
		suggestions: o,
		goalMode: i
	}), []), /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("label", {
		htmlFor: "live-prompt",
		className: "sr-only",
		children: r ? "Goal objective" : "Message Snow"
	}), /* @__PURE__ */ (0, D.jsx)("textarea", {
		id: "live-prompt",
		name: "text",
		rows: 1,
		maxLength: 65536,
		required: !0,
		placeholder: r ? "Describe the goal…" : "Message Snow…",
		"aria-describedby": "composer-hint composer-state live-reuse-notice",
		"aria-controls": a.controls,
		"aria-expanded": a.expanded,
		"aria-activedescendant": a.activeDescendant,
		value: t,
		onChange: (e) => n(e.currentTarget.value),
		onInputCapture: (e) => (0, u.flushSync)(() => n(e.currentTarget.value)),
		onCompositionStart: () => {
			s.current = !0;
		},
		onCompositionEnd: (e) => {
			s.current = !1, n(e.currentTarget.value);
		}
	})] });
}
function ba({ view: e }) {
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsxs)("div", {
			id: "live-regenerate-notice",
			className: "message-edit-notice",
			role: "status",
			hidden: !e.regenerate.visible,
			children: [/* @__PURE__ */ (0, D.jsx)("span", {
				"data-message-regenerate-outcome": "",
				children: e.regenerate.text
			}), /* @__PURE__ */ (0, D.jsx)("button", {
				type: "button",
				className: "quiet",
				"data-message-regenerate-dismiss": "",
				hidden: !e.regenerate.dismiss,
				children: "Dismiss"
			})]
		}),
		/* @__PURE__ */ (0, D.jsxs)("div", {
			id: "live-edit-notice",
			className: "message-edit-notice",
			role: "status",
			hidden: !e.edit.visible,
			children: [/* @__PURE__ */ (0, D.jsx)("span", {
				"data-message-edit-status": "",
				children: e.edit.text
			}), /* @__PURE__ */ (0, D.jsx)("button", {
				type: "button",
				className: "quiet",
				"data-message-edit-cancel": "",
				disabled: e.edit.disabled,
				children: "Cancel"
			})]
		}),
		/* @__PURE__ */ (0, D.jsxs)("div", {
			id: "live-reuse-notice",
			className: "message-reuse-notice",
			role: "status",
			hidden: !e.reuse.visible,
			children: [/* @__PURE__ */ (0, D.jsx)("span", {
				"data-message-reuse-status": "",
				children: e.reuse.text
			}), /* @__PURE__ */ (0, D.jsx)("button", {
				type: "button",
				className: "quiet",
				"data-message-reuse-cancel": "",
				disabled: e.reuse.disabled,
				children: "Cancel edit · restore draft"
			})]
		})
	] });
}
function xa({ view: e }) {
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("button", {
		className: "primary composer-send icon-button" + (e.sending ? " is-sending" : ""),
		type: "submit",
		id: "live-send",
		"aria-label": e.sendLabel,
		title: e.sendLabel,
		disabled: e.sendDisabled,
		hidden: e.showStop,
		children: [/* @__PURE__ */ (0, D.jsx)("svg", {
			className: "icon" + (e.sending ? " composer-send-progress" : ""),
			viewBox: "0 0 24 24",
			fill: "none",
			stroke: "currentColor",
			strokeWidth: "1.5",
			"aria-hidden": "true",
			children: e.sending ? /* @__PURE__ */ (0, D.jsx)("circle", {
				cx: "12",
				cy: "12",
				r: "7",
				strokeDasharray: "32 12"
			}) : /* @__PURE__ */ (0, D.jsx)("path", { d: "M12 19V5m-6 6 6-6 6 6" })
		}), /* @__PURE__ */ (0, D.jsx)("span", {
			"data-sending-label": "",
			className: "sr-only",
			hidden: !e.sending,
			children: "Sending…"
		})]
	}), /* @__PURE__ */ (0, D.jsxs)("button", {
		type: "button",
		className: "primary composer-send composer-stop",
		"data-runtime-abort": "",
		"data-stop-label": e.stopLabel,
		"aria-label": e.stopLabel,
		title: e.stopTitle,
		hidden: !e.showStop,
		disabled: !e.canStop,
		children: [/* @__PURE__ */ (0, D.jsx)("svg", {
			className: "icon",
			viewBox: "0 0 24 24",
			fill: "currentColor",
			"aria-hidden": "true",
			children: /* @__PURE__ */ (0, D.jsx)("rect", {
				x: "7",
				y: "7",
				width: "10",
				height: "10",
				rx: "1.5"
			})
		}), /* @__PURE__ */ (0, D.jsx)("span", {
			className: "sr-only",
			children: e.stopLabel
		})]
	})] });
}
function Sa({ view: e }) {
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsx)("span", {
			className: "composer-meta" + (e.statusIdle ? " is-idle" : ""),
			id: "composer-state",
			role: "status",
			children: e.status
		}),
		e.queue.enabled && /* @__PURE__ */ (0, D.jsx)("button", {
			type: "button",
			className: "quiet queue-next-button",
			"data-queue-next": "",
			hidden: e.queue.hidden,
			disabled: e.queue.disabled,
			title: e.queue.title,
			children: e.queue.label
		}),
		/* @__PURE__ */ (0, D.jsx)("span", {
			className: "composer-hint",
			id: "composer-hint",
			children: e.queue.hint
		})
	] });
}
function Ca({ visible: e }) {
	return /* @__PURE__ */ (0, D.jsx)("div", {
		id: "live-turn-status",
		className: "turn-status",
		role: "status",
		"aria-live": "polite",
		hidden: !e,
		children: "Working…"
	});
}
//#endregion
//#region src/live-view/Regeneration.tsx
function wa({ view: e }) {
	return /* @__PURE__ */ (0, D.jsxs)("dialog", {
		id: "message-regenerate-dialog",
		className: "folder-dialog",
		"aria-labelledby": "message-regenerate-title",
		"aria-describedby": "message-regenerate-description",
		children: [
			/* @__PURE__ */ (0, D.jsx)("div", {
				className: "dialog-heading",
				children: /* @__PURE__ */ (0, D.jsx)("h2", {
					id: "message-regenerate-title",
					children: "Regenerate this reply?"
				})
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				id: "message-regenerate-description",
				children: "Regenerating restarts this whole reply from its original prompt, including its earlier text and tool work, and replaces the following conversation. Tools may run again; earlier file changes are not undone. Your unsent draft is kept."
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				"data-message-regenerate-status": "",
				role: "status",
				"aria-live": "polite",
				children: e.status
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-actions",
				children: [/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-message-regenerate-cancel": "",
					autoFocus: !0,
					disabled: e.cancelDisabled,
					children: "Cancel"
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "button danger",
					"data-message-regenerate-confirm": "",
					disabled: e.confirmDisabled,
					children: "Regenerate"
				})]
			})
		]
	});
}
//#endregion
//#region src/live-view/model.ts
var Ta = () => ({
	connection: "Connecting…",
	connected: !1,
	reload: !1,
	error: "",
	unknown: !1,
	reviewDisabled: !0,
	recoveryVisible: !1,
	recoveryMessage: "",
	canceled: !1
}), Ea = () => ({
	sendDisabled: !0,
	showStop: !1,
	sendLabel: "Send message",
	sending: !1,
	goalMode: !1,
	canStop: !1,
	stopLabel: "Stop",
	stopTitle: "Stop generation",
	status: "Updates unavailable · your draft is kept",
	statusIdle: !1,
	turnVisible: !1,
	queue: {
		enabled: !1,
		hidden: !0,
		disabled: !0,
		label: "Queue next",
		title: "",
		hint: "Ctrl / ⌘ + Enter to send · Enter for a new line"
	},
	reuse: {
		visible: !1,
		text: "",
		disabled: !1
	},
	edit: {
		visible: !1,
		text: "",
		disabled: !1
	},
	regenerate: {
		visible: !1,
		text: "",
		dismiss: !1
	},
	dialog: {
		status: "Preparing confirmation…",
		confirmDisabled: !0,
		cancelDisabled: !1
	}
}), Da = null;
function Oa(e, t, n) {
	let r = e.roots.get(t);
	r && (0, u.flushSync)(() => r.render(n));
}
function ka() {
	let e = Da;
	if (Da = null, e) {
		for (let t of e.roots.values()) (0, u.flushSync)(() => t.unmount());
		e.roots.clear();
	}
}
function Aa(e) {
	if (Da?.region === e) return;
	ka();
	let t = {
		region: e,
		roots: /* @__PURE__ */ new Map(),
		draft: (0, l.createRef)(),
		chrome: Ta(),
		controls: Ea(),
		regeneration: e.dataset.messageRegenerateEnabled === "true"
	};
	t.controls.queue.enabled = e.dataset.queueNextEnabled === "true";
	for (let n of [
		"chrome",
		"composer-notices",
		"composer-editor",
		"composer-actions",
		"composer-status",
		"turn-status",
		"regenerate-dialog"
	]) {
		let r = e.querySelector(`#live-${n}-view`);
		r && (n === "chrome" && (t.chrome.error = (r.dataset.initialError || "").slice(0, 65536)), t.roots.set(n, (0, d.createRoot)(r)));
	}
	Da = t, Oa(t, "composer-editor", /* @__PURE__ */ (0, D.jsx)(ya, { ref: t.draft })), ja({}), Ma({});
}
function ja(e) {
	let t = Da;
	if (!t) return;
	let n = Object.keys(e).length === 0 || Object.keys(e).some((n) => e[n] !== t.chrome[n]);
	t.chrome = {
		...t.chrome,
		...e
	}, n && Oa(t, "chrome", /* @__PURE__ */ (0, D.jsx)(va, { view: t.chrome }));
}
function Ma(e) {
	let t = Da;
	if (!t) return;
	let n = t.controls, r = Object.keys(e).length === 0, i = (...t) => r || t.some((t) => t in e && e[t] !== n[t]);
	t.controls = {
		...n,
		...e
	};
	let a = t.controls;
	i("goalMode") && (0, u.flushSync)(() => t.draft.current?.goalMode(a.goalMode)), i("reuse", "edit", "regenerate") && Oa(t, "composer-notices", /* @__PURE__ */ (0, D.jsx)(ba, { view: a })), i("sendDisabled", "showStop", "sendLabel", "sending", "canStop", "stopLabel", "stopTitle") && Oa(t, "composer-actions", /* @__PURE__ */ (0, D.jsx)(xa, { view: a })), i("status", "statusIdle", "queue") && Oa(t, "composer-status", /* @__PURE__ */ (0, D.jsx)(Sa, { view: a })), i("turnVisible") && Oa(t, "turn-status", /* @__PURE__ */ (0, D.jsx)(Ca, { visible: a.turnVisible })), t.regeneration && i("dialog") && Oa(t, "regenerate-dialog", /* @__PURE__ */ (0, D.jsx)(wa, { view: a.dialog }));
}
function Na(e) {
	let t = Da;
	if (!t?.region.isConnected || !t.draft.current || typeof e != "string") return !1;
	let n = !1;
	return (0, u.flushSync)(() => {
		n = t.draft.current.update(e);
	}), n;
}
function Pa(e) {
	let t = Da;
	t?.draft.current && (0, u.flushSync)(() => t.draft.current.suggestions(e));
}
var Fa = {
	init: Aa,
	mount: Aa,
	updateChrome: ja,
	updateControls: Ma,
	updateDraft: Na,
	updateSuggestions: Pa,
	dispose: ka,
	clear: ka
}, Ia = 65536, La = /* @__PURE__ */ new Set([
	"image/png",
	"image/jpeg",
	"image/gif",
	"image/webp"
]), Ra = (e, t = Ia) => typeof e == "string" ? e.slice(0, t) : "", za = (e) => e && typeof e == "object" && !Array.isArray(e) ? e : {};
function Ba(e, t) {
	let n = typeof e == "string" ? e : "", r = 0, i = 0;
	for (let e of n) {
		let n = e.codePointAt(0), a = n <= 127 ? 1 : n <= 2047 ? 2 : n <= 65535 ? 3 : 4;
		if (r + a > t) break;
		r += a, i += e.length;
	}
	return {
		text: n.slice(0, i),
		bytes: r,
		truncated: i < n.length
	};
}
function Va(e) {
	let t = Array.isArray(e) ? e.slice(-100) : [], n = /* @__PURE__ */ new Set(), r = /* @__PURE__ */ new Set(), i = 0, a = [];
	for (let [e, o] of t.entries()) {
		let t = za(o), s = t.role;
		if (s !== "user" && s !== "assistant" && s !== "plan" && s !== "tool_activity") continue;
		let c = s === "tool_activity" ? typeof t.id == "string" && t.id.length <= 256 ? t.id : "" : Ra(t.id, 256) || `message-${e}`;
		if (!c || n.has(c)) continue;
		n.add(c);
		let l = (Array.isArray(t.images) ? t.images.slice(0, 8) : []).map((e) => {
			let t = za(e);
			return {
				index: Number.isSafeInteger(t.index) && Number(t.index) >= 0 && Number(t.index) <= 1e4 ? Number(t.index) : -1,
				mime: typeof t.mime_type == "string" && La.has(t.mime_type) ? t.mime_type : "",
				url: typeof t.url == "string" && t.url.length <= 4096 ? t.url : null
			};
		}), u = typeof t.text == "string" ? t.text : "", d = Ra(u), f = !!t.truncated || u.length > 65536, p = [], m = s === "assistant" && Array.isArray(t.tools) ? t.tools : [], h = t.tools_omitted === !0 || m.length > 64;
		for (let e of m.slice(0, 64)) {
			let t = za(e), n = Ra(t.id, 256);
			if (!n || r.has(n)) continue;
			if (r.size >= 64) {
				h = !0;
				break;
			}
			r.add(n);
			let a = t.status === "completed" || t.status === "failed" ? t.status : "unresolved", o = t.output_available === !0, s = Ba(a !== "unresolved" && o ? t.output : "", Math.min(8192, 131072 - i));
			i += s.bytes;
			let c = a === "unresolved" ? "No result recorded; execution outcome unknown" : o ? s.text : "Public output was not recorded";
			p.push({
				id: n,
				tool: Ra(t.tool, 128),
				status: a,
				label: a === "completed" ? "Completed" : a === "failed" ? "Failed" : "Outcome unknown",
				summary: "",
				output: c,
				expandable: !!c || !!t.truncated || s.truncated,
				error: a === "failed",
				truncated: a !== "unresolved" && o && (!!t.truncated || s.truncated),
				available: o,
				rawTruncated: !!t.truncated || s.truncated,
				open: t.open === !0
			});
		}
		a.push({
			id: c,
			role: s,
			text: d,
			html: typeof t.html == "string" && t.html.length <= 131072 ? t.html : "",
			truncated: f,
			editable: s === "user" && !l.length && t.can_edit === !0,
			regeneratable: s === "assistant" && t.can_regenerate === !0 && !f,
			reusable: s === "user" && !l.length && !f && typeof t.text == "string" && new TextEncoder().encode(u).length <= 65536,
			images: l,
			tools: p,
			toolsOmitted: h
		});
	}
	return a;
}
var Ha = {
	pending: "Pending",
	queued: "Queued",
	running: "Running",
	waiting: "Waiting",
	permission: "Approval needed",
	completed: "Completed",
	complete: "Completed",
	success: "Completed",
	done: "Completed",
	failed: "Failed",
	error: "Failed",
	denied: "Rejected",
	rejected: "Rejected",
	failure: "Failed",
	cancelled: "Cancelled",
	canceled: "Cancelled",
	interrupted: "Interrupted",
	unknown: "Outcome unknown",
	aborted: "Cancelled"
};
function Ua(e) {
	let t = [], n = /* @__PURE__ */ new Set();
	for (let [r, i] of (Array.isArray(e) ? e.slice(-128) : []).entries()) {
		if (!i || typeof i != "object") continue;
		let e = za(i), a = Ra(e.id, 256) || `activity-${r}`;
		if (n.has(a)) continue;
		n.add(a);
		let o = Ra(e.status, 32), s = Ra(e.output, 16384), c = Ra(e.summary, 1024);
		t.push({
			messageID: typeof e.message_id == "string" && e.message_id.length <= 256 ? e.message_id : "",
			data: {
				id: a,
				tool: Ra(e.tool, 128),
				status: o,
				output: s,
				summary: c,
				label: o === "unknown" ? "Outcome unknown" : e.is_error ? "Failed" : Object.hasOwn(Ha, o) ? Ha[o] : "Status unavailable",
				error: !!e.is_error || [
					"error",
					"failed",
					"failure",
					"denied",
					"rejected"
				].includes(o),
				expandable: !!s || !!e.truncated,
				truncated: !!e.truncated || typeof e.output == "string" && e.output.length > 16384 || typeof e.summary == "string" && e.summary.length > 1024,
				open: e.open === !0
			}
		});
	}
	return t;
}
//#endregion
//#region src/messages/imageTransport.ts
function Wa(e) {
	return {
		project: e?.dataset.project || "",
		instance: e?.dataset.instance || "",
		session: e?.dataset.session || "",
		live: e?.id === "live-session"
	};
}
function Ga(e) {
	return JSON.stringify([
		e.project,
		e.instance,
		e.session,
		e.live
	]);
}
function Ka(e, t, n, r, i, a) {
	if (!La.has(i) || !Number.isSafeInteger(r) || r < 0 || r > 1e4 || typeof e != "string" || !e || e.length > 4096 || !n) return "";
	let { project: o, instance: s, session: c, live: l } = t;
	if (!o || !c) return "";
	try {
		let t = new URL(e, a);
		if (t.origin !== a || t.username || t.password || t.hash) return "";
		let i = `/projects/${encodeURIComponent(o)}`;
		if (l) {
			if (!s || t.pathname !== `${i}/runtime/images/${encodeURIComponent(n)}/${r}` || t.searchParams.getAll("instance_id").length !== 1 || t.searchParams.get("instance_id") !== s || t.searchParams.getAll("session_id").length !== 1 || t.searchParams.get("session_id") !== c || t.searchParams.getAll("turn_id").length > 1 || [...t.searchParams.keys()].some((e) => ![
				"instance_id",
				"session_id",
				"turn_id"
			].includes(e))) return "";
		} else if (t.pathname !== `${i}/sessions/${encodeURIComponent(c)}/images/${encodeURIComponent(n)}/${r}` || t.search) return "";
		return e !== t.pathname + t.search && e !== t.href ? "" : t.pathname + t.search;
	} catch {
		return "";
	}
}
var qa = 2097152, Ja = /* @__PURE__ */ new Set(), Ya = /* @__PURE__ */ new Set(), Xa, Za = !1;
function Qa() {
	Za || (Za = !0, queueMicrotask(() => {
		if (Za = !1, Xa && !Xa.current() && Xa.release(), !Xa) for (let e of Ya) {
			if (Ya.delete(e), !e.current()) {
				e.release();
				continue;
			}
			Xa = e, e.start();
			break;
		}
	}));
}
function $a(e, t, n, r) {
	let i = !1, a = !1, o = "", s = new AbortController(), c, l = () => !i && n(), u = () => {
		a = !0, clearTimeout(c), Ya.delete(f), Xa === f && (Xa = void 0), Qa();
	}, d = (e) => {
		a || i || (l() && r(e ? "loaded" : "unavailable", e ? o : ""), !e && o && (URL.revokeObjectURL(o), o = ""), u());
	}, f = {
		current: l,
		release: () => {
			i || (i = !0, s.abort(), clearTimeout(c), o && URL.revokeObjectURL(o), o = "", Ja.delete(f), u());
		},
		start: () => {
			r("loading", ""), c = setTimeout(() => {
				s.abort(), d(!1);
			}, 8e3), (async () => {
				try {
					let n = await fetch(e, {
						credentials: "same-origin",
						cache: "no-store",
						redirect: "error",
						signal: s.signal,
						headers: { Accept: t }
					});
					if (!n.ok || n.headers.get("content-type")?.split(";")[0].trim() !== t || Number(n.headers.get("content-length")) > qa || !n.body) throw Error("Unavailable image");
					let i = n.body.getReader(), c = [], u = 0;
					try {
						for (;;) {
							let { value: e, done: t } = await i.read();
							if (t) break;
							if (u += e.byteLength, u > qa || !l()) throw Error("Unavailable image");
							c.push(new Uint8Array(e));
						}
					} finally {
						await i.cancel().catch(() => {}), i.releaseLock();
					}
					if (!u || !l() || a) {
						d(!1);
						return;
					}
					o = URL.createObjectURL(new Blob(c, { type: t })), r("loading", o);
				} catch {
					s.abort(), d(!1);
				}
			})();
		}
	};
	return Ja.size >= 800 ? (queueMicrotask(() => {
		l() && r("unavailable", "");
	}), {
		release: f.release,
		decoded: d
	}) : (Ja.add(f), Ya.add(f), Qa(), {
		release: f.release,
		decoded: d
	});
}
//#endregion
//#region src/messages/Images.tsx
function eo({ image: e, position: t, root: n, messageID: r }) {
	let i = (0, l.useRef)(null), a = (0, l.useRef)(void 0), o = Wa(n), s = Ga(o), c = Ka(e.url, o, r, e.index, e.mime, location.origin), d = La.has(e.mime) && e.index >= 0 && e.url === "", f = JSON.stringify([
		s,
		e.index,
		e.mime,
		c,
		d
	]), [p, m] = (0, l.useState)({
		identity: f,
		state: c ? "queued" : d ? "pending" : "unavailable",
		src: ""
	}), h = p.identity === f ? p : {
		identity: f,
		state: c ? "queued" : d ? "pending" : "unavailable",
		src: ""
	};
	return (0, l.useLayoutEffect)(() => {
		if (!c) return;
		let t = !1;
		return a.current = $a(c, e.mime, () => !t && !!i.current?.isConnected && Ga(Wa(n)) === s, (e, n) => {
			t || (0, u.flushSync)(() => m({
				identity: f,
				state: e,
				src: n
			}));
		}), () => {
			t = !0, a.current?.release(), a.current = void 0;
		};
	}, [
		f,
		n,
		c,
		e.mime,
		s
	]), /* @__PURE__ */ (0, D.jsxs)("div", {
		ref: i,
		className: "message-image",
		"data-image-index": e.index,
		"data-image-mime": e.mime,
		"data-image-state": h.state,
		children: [/* @__PURE__ */ (0, D.jsx)("img", {
			className: "message-image-preview",
			width: 96,
			height: 96,
			decoding: "async",
			loading: "eager",
			alt: `Attached image ${t + 1}`,
			"data-image-url": c || void 0,
			src: h.src || void 0,
			hidden: h.state !== "loaded",
			onLoad: () => a.current?.decoded(!0),
			onError: () => a.current?.decoded(!1)
		}, f), /* @__PURE__ */ (0, D.jsx)("span", {
			className: "message-image-fallback",
			hidden: h.state === "loaded",
			children: h.state === "pending" ? "Image pending" : h.state === "unavailable" ? "Image unavailable" : "Loading image"
		})]
	});
}
function to({ images: e, root: t, messageID: n }) {
	return e.length ? /* @__PURE__ */ (0, D.jsx)("div", {
		className: "message-images",
		role: "group",
		"aria-label": "Attached images",
		children: e.map((e, r) => /* @__PURE__ */ (0, D.jsx)(eo, {
			image: e,
			position: r,
			root: t,
			messageID: n
		}, r))
	}) : null;
}
//#endregion
//#region src/messages/markdownModel.ts
var no = new Set("p br hr strong em del blockquote pre code h1 h2 h3 h4 h5 h6 ul ol li table thead tbody tr th td a".split(" "));
function ro(e) {
	try {
		let t = new URL(e);
		return ["http:", "https:"].includes(t.protocol) && t.hostname && !t.username && !t.password ? e : void 0;
	} catch {
		return;
	}
}
//#endregion
//#region src/messages/Markdown.tsx
function io({ children: e, content: t }) {
	let [n, r] = (0, l.useState)("Copy code"), i = (0, l.useRef)(0), a = (0, l.useRef)(void 0);
	(0, l.useEffect)(() => () => {
		i.current++, clearTimeout(a.current);
	}, []);
	async function o() {
		let e = ++i.current, n = "Copied";
		try {
			await navigator.clipboard.writeText(t);
		} catch {
			n = "Select and copy manually";
		}
		e === i.current && (clearTimeout(a.current), r(n), a.current = setTimeout(() => {
			e === i.current && r("Copy code");
		}, 2500));
	}
	return /* @__PURE__ */ (0, D.jsxs)("div", {
		className: "code-block",
		children: [/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "code-banner",
			children: [/* @__PURE__ */ (0, D.jsx)("span", {
				className: "code-language",
				children: "Code"
			}), /* @__PURE__ */ (0, D.jsx)("button", {
				type: "button",
				className: "quiet copy-code",
				"data-copy-code": "",
				"aria-label": "Copy code to clipboard",
				onClick: o,
				children: n
			})]
		}), /* @__PURE__ */ (0, D.jsx)("pre", { children: e })]
	});
}
function ao(e) {
	if (!e || e.length > 131072) return [];
	let t = document.createElement("template");
	t.innerHTML = e;
	let n = 0;
	function r(e, t, i) {
		return i > 64 ? [] : Array.from(e, (e, a) => {
			if (++n > 32768) return null;
			let o = `${t}.${a}`;
			if (e.nodeType === Node.TEXT_NODE) return e.textContent;
			if (!(e instanceof HTMLElement)) return null;
			let s = e.localName;
			if (!no.has(s)) return null;
			let c = r(e.childNodes, o, i + 1);
			return s === "pre" ? /* @__PURE__ */ (0, D.jsx)(io, {
				content: e.textContent || "",
				children: c
			}, o) : s === "table" ? /* @__PURE__ */ (0, D.jsx)("div", {
				className: "message-table-scroll",
				tabIndex: 0,
				role: "region",
				"aria-label": "Scrollable table",
				children: /* @__PURE__ */ (0, D.jsx)("table", { children: c })
			}, o) : s === "a" ? /* @__PURE__ */ (0, D.jsx)("a", {
				href: ro(e.getAttribute("href") || ""),
				rel: "nofollow noreferrer",
				children: c
			}, o) : (0, l.createElement)(s, { key: o }, ...c);
		});
	}
	return r(t.content.childNodes, "markdown", 0);
}
function oo({ text: e, html: t }) {
	let n = (0, l.useMemo)(() => t ? ao(t) : /* @__PURE__ */ (0, D.jsx)(io, {
		content: e,
		children: e
	}), [t, e]);
	return /* @__PURE__ */ (0, D.jsx)(D.Fragment, { children: n });
}
//#endregion
//#region src/messages/ToolRow.tsx
var so = {
	file: "M6 2h8l4 4v16H6z M14 2v5h4",
	tool: "m8 6-6 6 6 6 M16 6l6 6-6 6 M14 4l-4 16",
	chevron: "m9 5 7 7-7 7",
	search: "M16 10a6 6 0 1 1-12 0 6 6 0 0 1 12 0Zm-2 4 6 6",
	terminal: "m4 6 6 6-6 6 M13 18h7",
	edit: "m4 16 12-12 4 4-12 12-5 1z M13 7l4 4"
}, co = {
	glob: ["search", "Find files"],
	grep: ["search", "Search"],
	read: ["file", "Read"],
	bash: ["terminal", "Run"],
	write: ["edit", "Write"],
	edit: ["edit", "Edit"]
};
function lo({ kind: e, className: t }) {
	return /* @__PURE__ */ (0, D.jsx)("svg", {
		viewBox: "0 0 24 24",
		"aria-hidden": "true",
		className: `inspection-icon ${t}`,
		"data-kind": e,
		fill: "none",
		stroke: "currentColor",
		strokeWidth: "1.5",
		children: /* @__PURE__ */ (0, D.jsx)("path", { d: so[e] || so.tool })
	});
}
function uo({ data: e }) {
	let t = e.tool || "Tool", [n, r] = Object.hasOwn(co, t) ? co[t] : ["tool", t], i = (e) => e.trim().replace(/\s+/g, " ").toLowerCase(), a = [t, r].some((t) => i(t) === i(e.summary)) ? "" : e.summary;
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("summary", {
		"aria-label": `${r === t ? t : `${r} (${t})`}${a ? ` · ${a}` : ""} · ${e.label}`,
		"aria-disabled": !e.expandable,
		tabIndex: e.expandable ? 0 : -1,
		onClick: (t) => {
			e.expandable || t.preventDefault();
		},
		children: [
			/* @__PURE__ */ (0, D.jsxs)("span", {
				className: "activity-leading",
				children: [/* @__PURE__ */ (0, D.jsx)(lo, {
					kind: n,
					className: "activity-kind"
				}), /* @__PURE__ */ (0, D.jsx)(lo, {
					kind: "chevron",
					className: "activity-chevron"
				})]
			}),
			/* @__PURE__ */ (0, D.jsx)("span", {
				className: "activity-tool",
				title: t,
				children: r
			}),
			/* @__PURE__ */ (0, D.jsx)("span", {
				className: "activity-separator",
				"aria-hidden": "true",
				hidden: !a
			}),
			/* @__PURE__ */ (0, D.jsx)("span", {
				className: "activity-summary",
				title: a,
				hidden: !a,
				children: a
			}),
			/* @__PURE__ */ (0, D.jsx)("span", {
				className: `activity-status${!e.error && [
					"completed",
					"complete",
					"success",
					"done"
				].includes(e.status) ? " activity-status-quiet" : ""}`,
				children: e.label
			})
		]
	}), /* @__PURE__ */ (0, D.jsxs)("div", {
		className: "activity-body",
		hidden: !e.expandable,
		children: [/* @__PURE__ */ (0, D.jsx)("pre", {
			className: "activity-output",
			children: e.output
		}), /* @__PURE__ */ (0, D.jsx)("p", {
			className: "fine activity-truncated",
			hidden: !e.truncated,
			children: "Tool output truncated for bounded display."
		})]
	})] });
}
function fo({ data: e, history: t = !1 }) {
	let n = (0, l.useRef)(null), r = (0, l.useRef)(!0);
	return (0, l.useLayoutEffect)(() => {
		n.current && (!e.expandable || r.current && e.open) && (n.current.open = e.expandable && !!e.open), r.current = !1;
	}, [e.expandable, e.open]), /* @__PURE__ */ (0, D.jsx)("details", {
		ref: n,
		className: `tool-activity${t ? " history-tool" : ""}${e.error ? " activity-error" : ""}${e.expandable ? "" : " activity-no-output"}`,
		"data-history-tool-id": t ? e.id : void 0,
		"data-activity-id": t ? void 0 : e.id,
		"data-tool-name": e.tool || "Tool",
		"data-status": e.status,
		"data-output-available": t ? String(e.available) : void 0,
		"data-truncated": t ? String(e.rawTruncated) : void 0,
		children: /* @__PURE__ */ (0, D.jsx)(uo, { data: e })
	});
}
//#endregion
//#region src/messages/Message.tsx
function po({ copied: e }) {
	return /* @__PURE__ */ (0, D.jsx)("svg", {
		viewBox: "0 0 24 24",
		"aria-hidden": "true",
		fill: "none",
		stroke: "currentColor",
		strokeWidth: "1.7",
		strokeLinecap: "round",
		strokeLinejoin: "round",
		children: /* @__PURE__ */ (0, D.jsx)("path", { d: e ? "m5 12 4 4L19 6" : "M9 8V5a2 2 0 0 1 2-2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-3M5 8h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-9a2 2 0 0 1 2-2Z" })
	});
}
function mo({ text: e }) {
	let [t, n] = (0, l.useState)(""), r = (0, l.useRef)(0), i = (0, l.useRef)(void 0);
	(0, l.useLayoutEffect)(() => () => {
		r.current++, clearTimeout(i.current);
	}, []);
	async function a() {
		let t = ++r.current, a = "Copied";
		try {
			await navigator.clipboard.writeText(e);
		} catch {
			a = "Could not copy. Select the message and copy manually.";
		}
		t === r.current && (clearTimeout(i.current), n(a), i.current = setTimeout(() => {
			t === r.current && n("");
		}, 2500));
	}
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("button", {
		type: "button",
		className: "message-copy",
		"data-message-copy": "",
		"aria-label": t === "Copied" ? "Message copied" : "Copy message",
		title: t === "Copied" ? "Copied" : "Copy message",
		onClick: a,
		children: /* @__PURE__ */ (0, D.jsx)(po, { copied: t === "Copied" })
	}), /* @__PURE__ */ (0, D.jsx)("span", {
		className: "message-copy-feedback",
		role: "status",
		"aria-live": "polite",
		children: t
	})] });
}
function ho({ message: e, scope: t, actions: n }) {
	let r = (0, l.useRef)(null), { role: i } = e, a = t?.id === "live-session" && t.dataset.runtime === "true", o = t?.id === "live-session" && t.dataset.messageEditEnabled === "true", s = t?.id === "live-session" && t.dataset.messageRegenerateEnabled === "true";
	return (0, l.useLayoutEffect)(() => {
		if (!r.current) return;
		let t = r.current;
		t._snowCopyText = e.text, t._snowHasImages = !!e.images.length, t._snowEditable = e.editable, t._snowRegeneratable = e.regeneratable, t._snowReusable = e.reusable;
	}), /* @__PURE__ */ (0, D.jsxs)("article", {
		ref: r,
		className: `catalog-message conversation-message${i === "user" ? " user-message" : ""}`,
		"data-message-id": e.id,
		"data-message-role": i,
		"data-message-editable": String(e.editable),
		"data-message-has-images": String(!!e.images.length),
		"data-message-regeneratable": String(e.regeneratable),
		"data-editing": String(a && i === "user" && n.edit.messageID === e.id),
		"aria-label": i === "user" ? "Your message" : i === "plan" ? "Assistant plan" : "Assistant message",
		children: [
			/* @__PURE__ */ (0, D.jsx)(to, {
				images: e.images,
				root: t,
				messageID: e.id
			}),
			/* @__PURE__ */ (0, D.jsx)("div", {
				className: `message-body${i === "user" ? "" : " markdown-body"}`,
				hidden: !e.text && !!e.images.length,
				children: i === "user" ? e.text : /* @__PURE__ */ (0, D.jsx)(oo, {
					text: e.text,
					html: e.html
				})
			}),
			/* @__PURE__ */ (0, D.jsx)("span", {
				className: "message-source",
				hidden: !0,
				children: e.text
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine message-truncated",
				hidden: !e.truncated,
				children: "Message truncated for bounded display."
			}),
			(e.tools.length > 0 || e.toolsOmitted) && /* @__PURE__ */ (0, D.jsxs)("div", {
				className: "message-tools tool-timeline activity-list",
				role: "group",
				"aria-label": "Saved tool history",
				children: [e.tools.map((e) => /* @__PURE__ */ (0, D.jsx)(fo, {
					data: e,
					history: !0
				}, e.id)), /* @__PURE__ */ (0, D.jsx)("p", {
					className: "fine history-tool-limit",
					hidden: !e.toolsOmitted,
					children: "Saved tool history truncated for bounded display."
				})]
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "message-actions",
				children: [
					i === "user" && !e.images.length && (o ? e.editable && /* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "message-edit",
						"data-message-edit": "",
						disabled: !a || n.edit.disabled || n.historicalDisabled || !e.editable,
						hidden: !a || n.edit.hidden,
						title: "Edit this message and replace the following conversation when you Send.",
						children: "Edit & resend"
					}) : /* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "message-reuse",
						"data-message-reuse": "",
						disabled: !a || n.reuse.disabled || n.historicalDisabled || !e.reusable,
						hidden: !a || n.reuse.hidden,
						title: e.reusable ? n.reuse.title : "This message is truncated and cannot be reused",
						children: "Use as new prompt"
					})),
					s && e.regeneratable && /* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "message-regenerate",
						"data-message-regenerate": "",
						disabled: !a || n.regenerate.disabled || !e.regeneratable,
						hidden: !a || n.regenerate.hidden,
						title: "Regenerate this reply and replace the following conversation after confirmation.",
						children: "Regenerate"
					}),
					/* @__PURE__ */ (0, D.jsx)(mo, { text: e.text })
				]
			})
		]
	});
}
//#endregion
//#region src/messages/actions.ts
var go = "Use a copy as a new prompt. Saved history stays unchanged.";
function _o() {
	return {
		reuse: {
			hidden: !0,
			disabled: !0,
			title: go
		},
		edit: {
			hidden: !0,
			disabled: !0,
			messageID: ""
		},
		regenerate: {
			hidden: !0,
			disabled: !0
		},
		historicalDisabled: !1
	};
}
function vo(e, t) {
	let n = za(t), r = za(n.reuse), i = za(n.edit), a = za(n.regenerate), o = (e, t, n) => Object.hasOwn(e, t) ? typeof e[t] != "boolean" || e[t] : n, s = {
		reuse: {
			hidden: o(r, "hidden", e.reuse.hidden),
			disabled: o(r, "disabled", e.reuse.disabled),
			title: Object.hasOwn(r, "title") ? Ra(r.title, 512) || "Use a copy as a new prompt. Saved history stays unchanged." : e.reuse.title
		},
		edit: {
			hidden: o(i, "hidden", e.edit.hidden),
			disabled: o(i, "disabled", e.edit.disabled),
			messageID: Object.hasOwn(i, "messageID") ? typeof i.messageID == "string" && i.messageID.length <= 256 ? i.messageID : "" : e.edit.messageID
		},
		regenerate: {
			hidden: o(a, "hidden", e.regenerate.hidden),
			disabled: o(a, "disabled", e.regenerate.disabled)
		},
		historicalDisabled: o(n, "historicalDisabled", e.historicalDisabled)
	};
	return JSON.stringify(s) === JSON.stringify(e) ? e : s;
}
//#endregion
//#region src/messages/initial.ts
function yo(e) {
	let t = [];
	for (let n of Array.from(e.children).slice(-100)) {
		if (!(n instanceof HTMLElement)) continue;
		if (n.hasAttribute("data-runtime-activity-group")) {
			t.push({
				id: n.dataset.messageId,
				role: "tool_activity"
			});
			continue;
		}
		if (!n.classList.contains("catalog-message")) continue;
		let e = n.querySelector(".message-body") || n.querySelector("pre"), r = n.querySelector(".message-source"), i = n.dataset.messageRole || (n.classList.contains("user-message") ? "user" : n.querySelector(".message-label")?.textContent?.trim().toLowerCase() || "assistant"), a = Array.from(n.querySelectorAll(".message-tools > .history-tool")).slice(0, 64).map((e) => ({
			id: e.dataset.historyToolId,
			tool: e.dataset.toolName || e.querySelector(".activity-tool")?.textContent,
			status: e.dataset.status,
			output_available: e.dataset.outputAvailable === "true",
			output: e.querySelector(".activity-output")?.textContent,
			truncated: e.dataset.truncated === "true",
			open: e.open
		})), o = Array.from(n.querySelectorAll(".message-images > .message-image")).slice(0, 8).map((e) => ({
			index: Number(e.dataset.imageIndex),
			mime_type: e.dataset.imageMime,
			url: e.querySelector(".message-image-preview")?.dataset.imageUrl || ""
		}));
		t.push({
			id: n.dataset.messageId,
			role: i,
			text: r ? r.textContent || "" : e?.textContent || "",
			html: i === "user" ? "" : e?.innerHTML || "",
			can_edit: n.dataset.messageEditable === "true",
			can_regenerate: n.dataset.messageRegeneratable === "true",
			truncated: n.dataset.messageTruncated === "true" || n.querySelector(".message-truncated")?.hidden === !1 || Array.from(n.querySelectorAll(":scope > p.fine")).some((e) => e.textContent === "This message is truncated for bounded display. The original session is unchanged."),
			images: o,
			tools: a,
			tools_omitted: n.querySelector(".history-tool-limit")?.hidden === !1,
			missing_source: !r
		});
	}
	let n = Va(t);
	for (let e of n) t.find((t, n) => (Ra(t.id, 256) || `message-${n}`) === e.id)?.missing_source && (e.reusable = !1);
	return n;
}
function bo(e) {
	return e ? Ua(Array.from(e.querySelectorAll("[data-activity-id]")).slice(-128).map((e) => ({
		id: e.dataset.activityId,
		message_id: e.dataset.messageId,
		tool: e.dataset.toolName || e.querySelector(".activity-tool")?.textContent,
		status: e.dataset.status,
		summary: e.querySelector(".activity-summary")?.textContent,
		output: e.querySelector(".activity-output")?.textContent,
		is_error: e.classList.contains("activity-error"),
		truncated: e.querySelector(".activity-truncated")?.hidden === !1,
		open: e.open
	}))) : [];
}
//#endregion
//#region src/messages/controller.tsx
var xo = /* @__PURE__ */ new Map(), So = /* @__PURE__ */ new WeakMap(), Co = /* @__PURE__ */ new Map();
function wo(e) {
	return e.closest("#live-session") || e.closest(".catalog-history");
}
function To(e) {
	let t = wo(e), n = t?.id === "live-session" ? t.querySelector("#live-activities") : null, r = Ga(Wa(t)), i = So.get(e), a = i?.identity === r ? i : void 0;
	So.delete(e);
	let o = a?.messages || yo(e), s = a?.activities || bo(n), c = a?.truncated ?? n?.querySelector(".activity-limit")?.hidden === !1, l = (0, d.createRoot)(e), u = {
		actions: _o(),
		root: l,
		host: e,
		scope: t,
		identity: Ga(Wa(t)),
		messages: o,
		activities: s,
		region: n,
		truncated: c,
		slots: /* @__PURE__ */ new Map(),
		noticeHost: null
	};
	return xo.set(e, u), n && Eo(u, n), u;
}
function Eo(e, t) {
	if (e.region !== t && (e.noticeHost?.remove(), e.noticeHost = null), e.region = t, !e.noticeHost) {
		t.replaceChildren();
		let n = document.createElement("div");
		n.style.display = "contents", n.dataset.reactActivityNotice = "", t.before(n), e.noticeHost = n;
	}
}
function Do(e) {
	let t = /* @__PURE__ */ new Map();
	for (let n of e.host.querySelectorAll(":scope > [data-runtime-activity-group]")) {
		let e = n.querySelector(".activity-list");
		n.dataset.messageId && e && t.set(n.dataset.messageId, e);
	}
	return t;
}
function Oo(e, t, n) {
	if (e.parentNode === t && e === n) return;
	let r = t;
	r.moveBefore && e.isConnected && t.isConnected ? r.moveBefore(e, n) : t.insertBefore(e, n);
}
function ko({ state: e }) {
	let t = new Set(e.messages.filter((e) => e.role === "tool_activity").map((e) => e.id)), n = new Set(e.activities.filter((e) => t.has(e.messageID)).map((e) => e.messageID));
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		e.scope?.id !== "live-session" && !e.messages.length && /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "empty-state compact",
			children: [/* @__PURE__ */ (0, D.jsx)("h2", { children: "No displayable messages" }), /* @__PURE__ */ (0, D.jsx)("p", { children: "This page has no displayable messages." })]
		}),
		e.messages.map((t) => t.role === "tool_activity" ? /* @__PURE__ */ (0, D.jsx)("section", {
			className: "runtime-tool-group tool-timeline",
			"data-message-id": t.id,
			"data-message-role": "tool_activity",
			"data-runtime-activity-group": "",
			role: "group",
			"aria-label": "Runtime tool activity",
			hidden: !n.has(t.id),
			children: /* @__PURE__ */ (0, D.jsx)("div", { className: "activity-list" })
		}, `${e.identity}:activity:${t.id}`) : /* @__PURE__ */ (0, D.jsx)(ho, {
			message: t,
			scope: e.scope,
			actions: e.actions
		}, `${e.identity}:message:${t.id}`)),
		e.region && (0, u.createPortal)(/* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "activity-provenance",
			children: [/* @__PURE__ */ (0, D.jsx)("h2", {
				id: "tool-timeline-heading",
				className: "sr-only",
				children: "Unassociated runtime tools"
			}), /* @__PURE__ */ (0, D.jsx)("span", { children: "Unassociated runtime tools" })]
		}), /* @__PURE__ */ (0, D.jsx)("div", { className: "activity-list" })] }), e.region),
		e.noticeHost && (0, u.createPortal)(/* @__PURE__ */ (0, D.jsx)("p", {
			className: "fine activity-limit",
			hidden: !e.truncated,
			children: "Earlier tool activity is omitted from this bounded view."
		}), e.noticeHost),
		e.region && e.activities.map((t) => (0, u.createPortal)(/* @__PURE__ */ (0, D.jsx)(fo, { data: t.data }), e.slots.get(t.data.id), `${e.identity}:tool:${t.data.id}`))
	] });
}
function Ao(e) {
	let t = document.activeElement instanceof HTMLElement ? document.activeElement : null, n = window.getSelection(), r = n && !n.isCollapsed && (e.host.contains(n.anchorNode) || e.region?.contains(n.anchorNode)) ? {
		anchor: n.anchorNode,
		start: n.anchorOffset,
		focus: n.focusNode,
		end: n.focusOffset
	} : null, i = new Set(e.activities.map((e) => e.data.id));
	for (let t of e.activities) if (!e.slots.has(t.data.id)) {
		let n = document.createElement("div");
		n.style.display = "contents", n.dataset.reactActivitySlot = t.data.id, e.slots.set(t.data.id, n);
	}
	let a = new Set(e.messages.filter((e) => e.role === "tool_activity").map((e) => e.id)), o = e.region?.querySelector(".activity-list");
	if (o) for (let [t, n] of e.slots) {
		let e = n.closest("[data-runtime-activity-group]");
		i.has(t) && e && !a.has(e.dataset.messageId || "") && Oo(n, o, null);
	}
	(0, u.flushSync)(() => e.root.render(/* @__PURE__ */ (0, D.jsx)(ko, { state: e })));
	let s = Do(e), c = e.region?.querySelector(".activity-list"), l = /* @__PURE__ */ new Map();
	if (c) for (let t of e.activities) {
		let n = s.get(t.messageID) || c, r = e.slots.get(t.data.id), i = l.get(n) || 0;
		Oo(r, n, n.childNodes[i] || null), l.set(n, i + 1);
	}
	for (let [t, n] of e.slots) i.has(t) || (n.remove(), e.slots.delete(t));
	if (e.region && (e.region.hidden = !c?.childElementCount), t?.isConnected && document.activeElement !== t && t.focus({ preventScroll: !0 }), n && r?.anchor?.isConnected && r.focus?.isConnected) {
		let e = (e) => e.nodeType === Node.TEXT_NODE ? e.textContent?.length || 0 : e.childNodes.length, t = Math.min(r.start, e(r.anchor)), i = Math.min(r.end, e(r.focus));
		(n.anchorNode !== r.anchor || n.anchorOffset !== t || n.focusNode !== r.focus || n.focusOffset !== i) && n.setBaseAndExtent(r.anchor, t, r.focus, i);
	}
}
function jo(e) {
	let t = xo.get(e);
	return t && t.identity !== Ga(Wa(wo(e))) && (Mo(t), t = void 0), t || To(e);
}
function Mo(e) {
	So.set(e.host, {
		identity: e.identity,
		messages: e.messages,
		activities: e.activities,
		truncated: e.truncated
	}), (0, u.flushSync)(() => e.root.unmount());
	for (let t of e.slots.values()) t.remove();
	e.noticeHost?.remove(), xo.delete(e.host);
}
function No(e, t) {
	if (!e || e.closest("[data-react-saved-history]")) return;
	let n = jo(e);
	n.messages = Va(t), Ao(n);
}
function Po(e, t) {
	if (!e?.isConnected || e.closest("[data-react-saved-history]")) return;
	let n = jo(e), r = vo(n.actions, t);
	r !== n.actions && (n.actions = r, Ao(n));
}
function Fo(e = document) {
	for (let e of xo.values()) e.host.isConnected || Mo(e);
	let t = /* @__PURE__ */ new Set();
	e instanceof HTMLElement && e.matches("[data-react-messages], #live-transcript") && t.add(e);
	for (let n of e.querySelectorAll("[data-react-messages], #live-transcript")) t.add(n);
	for (let n of e.querySelectorAll(".catalog-history .catalog-message")) n.parentElement && t.add(n.parentElement);
	for (let e of t) !e.closest("[data-react-saved-history], [data-react-page=\"workspace-cold\"]") && !xo.has(e) && Ao(jo(e));
}
function Io() {
	for (let e of [...xo.values()]) Mo(e);
	for (let e of Co.values()) (0, u.flushSync)(() => e.unmount());
	Co.clear();
}
function Lo(e) {
	for (let e of [...xo.values()]) e.host.isConnected || Mo(e);
	e && Fo(e);
}
function Ro(e, t, n = document.querySelector("#live-transcript")) {
	if (!e || !n || n.closest("[data-react-saved-history]")) return;
	let r = jo(n), i = za(t);
	(r.region !== e || !r.noticeHost) && Eo(r, e), r.activities = Ua(i.activities), r.truncated = !!i.activities_truncated || Array.isArray(i.activities) && i.activities.length > 128, Ao(r);
}
function zo(e, t) {
	if (!e || e.closest("[data-react-messages], #live-transcript, [data-react-saved-history]")) return;
	let n = za(t), r = {
		id: "",
		tool: Ra(n.tool, 128),
		status: Ra(n.status, 32),
		label: Ra(n.label, 128),
		summary: Ra(n.summary, 1024),
		output: Ra(n.output, 16384),
		error: !!n.error,
		expandable: !!n.expandable,
		truncated: !!n.truncated
	}, i = Co.get(e);
	i || (i = (0, d.createRoot)(e), Co.set(e, i)), e.dataset.status = r.status, e.dataset.toolName = r.tool || "Tool", e.classList.toggle("activity-error", r.error), e.classList.toggle("activity-no-output", !r.expandable), r.expandable || (e.open = !1), (0, u.flushSync)(() => i.render(/* @__PURE__ */ (0, D.jsx)(uo, { data: r })));
}
var Bo = Object.freeze({
	render: No,
	enhance: Fo,
	init: Lo,
	dispose: Io,
	updateActions: Po
}), Vo = Object.freeze({
	renderActivities: Ro,
	renderToolRow: zo
}), Ho = Symbol("saved-history"), Uo = /* @__PURE__ */ new WeakMap(), Wo = (e) => typeof e == "string" && /^[A-Za-z0-9_-]{1,128}$/.test(e), z = _o();
function B(e, t) {
	if (!e || !Wo(t.project) || !Wo(t.session) || e.closest("#live-session, [data-react-saved-history]")) return null;
	let n = e.closest(".catalog-history");
	if (!n) return null;
	let r = Wa(n);
	if (r.live || r.instance || r.project !== t.project || r.session !== t.session) return null;
	let i = yo(e.querySelector(":scope > [data-react-messages]") || e).filter((e) => e.role !== "tool_activity"), a = Object.freeze({ [Ho]: !0 });
	return Uo.set(a, {
		project: r.project,
		session: r.session,
		messages: i
	}), a;
}
function Go({ history: e }) {
	let [t, n] = (0, l.useState)(null);
	return /* @__PURE__ */ (0, D.jsx)("div", {
		ref: n,
		className: "catalog-history",
		"data-project": e.project,
		"data-session": e.session,
		"data-react-saved-history": "",
		children: e.messages.length ? e.messages.map((e) => /* @__PURE__ */ (0, D.jsx)(ho, {
			message: e,
			scope: t,
			actions: z
		}, e.id)) : /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "empty-state compact",
			children: [/* @__PURE__ */ (0, D.jsx)("h2", { children: "No displayable messages" }), /* @__PURE__ */ (0, D.jsx)("p", { children: "This page has no displayable messages." })]
		})
	});
}
function Ko({ projection: e }) {
	let t = e && Uo.get(e);
	return t ? /* @__PURE__ */ (0, D.jsx)(Go, { history: t }, `${t.project}:${t.session}`) : null;
}
//#endregion
//#region src/inspection/model.ts
var qo = (e) => !!e && typeof e == "object" && !Array.isArray(e), V = (e, t = 4096) => typeof e == "string" && e.length <= t, Jo = (e) => Number.isSafeInteger(e) && e >= 0;
function Yo(e) {
	return !qo(e) || !qo(e.project) || !V(e.project.id, 128) || !e.project.id || !V(e.project.name, 128) || !V(e.project.path) || typeof e.project.available != "boolean" || !V(e.csrf, 512) || e.live !== void 0 && (!qo(e.live) || !V(e.live.session_id, 512) || !V(e.live.provider, 256) || !V(e.live.model, 512)) ? null : e;
}
function Xo(e, t, n) {
	return !qo(e) || !V(e.project_id, 128) || !e.project_id || !V(e.instance_id, 256) || !e.instance_id || !V(e.session_id, 512) || !e.session_id || !V(e.provider, 256) || !V(e.model, 512) || e.project_id !== t || e.project_id !== n.project_id || e.instance_id !== n.instance_id || e.session_id !== n.session_id ? null : {
		session_id: e.session_id,
		provider: e.provider,
		model: e.model
	};
}
function Zo(e) {
	return qo(e) && V(e.path) && Array.isArray(e.entries) && e.entries.length <= 256 && e.entries.every((e) => qo(e) && V(e.name) && V(e.path) && (e.kind === "file" || e.kind === "directory")) && Jo(e.next_offset) && typeof e.has_more == "boolean" && typeof e.limited == "boolean";
}
function Qo(e) {
	return qo(e) && V(e.path) && V(e.text, 65536) && Jo(e.size) && typeof e.truncated == "boolean";
}
function $o(e) {
	return qo(e) && typeof e.available == "boolean" && V(e.reason, 2048) && typeof e.limited == "boolean" && Array.isArray(e.changes) && e.changes.length <= 256 && e.changes.every((e) => qo(e) && V(e.path) && [
		"staged",
		"unstaged",
		"untracked"
	].includes(String(e.kind)) && V(e.status, 32));
}
function es(e) {
	return qo(e) && typeof e.available == "boolean" && V(e.reason, 2048) && V(e.path) && V(e.kind, 32) && V(e.text, 65536) && typeof e.truncated == "boolean";
}
var ts = new Intl.Collator(void 0, {
	numeric: !0,
	sensitivity: "base"
});
function ns(e, t, n) {
	let r = /* @__PURE__ */ new Map();
	for (let i of [...n ? e : [], ...t]) {
		if (r.size >= 4096) break;
		r.set(i.path, i);
	}
	return [...r.values()].sort((e, t) => Number(t.kind === "directory") - Number(e.kind === "directory") || ts.compare(e.path, t.path));
}
function rs(e) {
	let t = e.slice(0, 65536), n = [], r = 0;
	for (let e of t.matchAll(/[^\n]*\n|[^\n]+$/g)) {
		if (n.length === 4096) {
			n.push({
				kind: "context",
				text: t.slice(r)
			});
			break;
		}
		let i = e[0];
		r += i.length, n.push({
			kind: /^(diff |index |--- |\+\+\+ )/.test(i) ? "meta" : i.startsWith("@@") ? "hunk" : i.startsWith("+") ? "added" : i.startsWith("-") ? "removed" : "context",
			text: i
		});
	}
	return n;
}
//#endregion
//#region src/inspection/Panel.tsx
var is = {
	folder: "M2 5h5l2 2h13v12H2z",
	file: "M6 2h8l4 4v16H6z M14 2v5h4",
	chevron: "m9 5 7 7-7 7",
	close: "m6 6 12 12M18 6 6 18",
	up: "m5 11 7-7 7 7M12 4v16",
	refresh: "M20 7v5h-5M4 17v-5h5M5 8a8 8 0 0 1 14-2l1 6M4 12l1 6a8 8 0 0 0 14-2"
};
function as({ kind: e }) {
	return /* @__PURE__ */ (0, D.jsx)("svg", {
		className: "inspection-icon",
		viewBox: "0 0 24 24",
		fill: "none",
		stroke: "currentColor",
		strokeWidth: "1.5",
		"aria-hidden": "true",
		children: /* @__PURE__ */ (0, D.jsx)("path", { d: is[e] })
	});
}
function os({ value: e }) {
	let t = e.slice(0, 4096), n = t.lastIndexOf("/"), r = t.slice(n + 1), i = r.lastIndexOf(".");
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [n >= 0 && /* @__PURE__ */ (0, D.jsx)("span", {
		className: "inspection-directory",
		children: t.slice(0, n + 1)
	}), /* @__PURE__ */ (0, D.jsxs)("span", {
		className: "inspection-basename",
		children: [/* @__PURE__ */ (0, D.jsx)("span", {
			className: "inspection-stem",
			children: i > 0 ? r.slice(0, i) : r
		}), i > 0 && /* @__PURE__ */ (0, D.jsx)("span", {
			className: "inspection-extension",
			children: r.slice(i)
		})]
	})] });
}
function ss({ view: e, kind: t }) {
	let n = e.notices[t];
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("p", {
		className: "fine",
		[`data-${t}-status`]: "",
		role: "status",
		"data-state": n.error ? "error" : n.status.startsWith("Reading") ? "loading" : "ready",
		children: n.status
	}), /* @__PURE__ */ (0, D.jsx)("p", {
		className: "error",
		[`data-${t}-error`]: "",
		role: "alert",
		hidden: !n.error,
		children: n.error
	})] });
}
function cs({ view: e, kind: t }) {
	let n = e[t], r = t === "diff";
	return /* @__PURE__ */ (0, D.jsxs)("section", {
		className: "inspection-preview",
		[`data-${t}-preview`]: "",
		hidden: !n,
		"aria-label": r ? "Change preview" : "File preview",
		children: [
			/* @__PURE__ */ (0, D.jsx)("div", {
				className: "inspection-preview-heading",
				children: /* @__PURE__ */ (0, D.jsxs)("span", {
					className: "fine",
					children: [r ? "Diff" : "File", " preview · read only"]
				})
			}),
			/* @__PURE__ */ (0, D.jsx)("h3", {
				className: "inspection-path",
				[`data-${t}-title`]: "",
				title: n?.path,
				children: /* @__PURE__ */ (0, D.jsx)(os, { value: n?.path || "" })
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				[`data-${t}-notice`]: "",
				children: n?.notice
			}),
			/* @__PURE__ */ (0, D.jsx)("pre", {
				className: "inspection-code",
				[`data-${t}-content`]: "",
				tabIndex: 0,
				"aria-label": r ? "Read-only diff" : "Read-only file content",
				children: r ? rs(n?.text || "").map((e, t) => /* @__PURE__ */ (0, D.jsx)("span", {
					className: `inspection-diff-line inspection-diff-${e.kind}`,
					children: e.text
				}, t)) : n?.text
			})
		]
	});
}
function ls({ view: e, actions: t }) {
	let n = [
		"files",
		"changes",
		"project"
	], r = e.path === "." ? [] : e.path.split("/").filter((e) => e && e !== "."), i = e.props, a = e.filesGeneration, o = e.changesGeneration;
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "section-heading",
			children: [/* @__PURE__ */ (0, D.jsx)("span", {
				className: "inspection-heading",
				children: "Project inspector"
			}), /* @__PURE__ */ (0, D.jsx)("button", {
				className: "quiet",
				type: "button",
				"data-inspector-toggle": !0,
				"aria-label": "Close project details",
				title: "Close project details",
				children: /* @__PURE__ */ (0, D.jsx)(as, { kind: "close" })
			})]
		}),
		/* @__PURE__ */ (0, D.jsx)("div", {
			className: "inspector-tabs",
			role: "tablist",
			"aria-label": "Project inspection",
			children: n.map((r) => /* @__PURE__ */ (0, D.jsx)("button", {
				type: "button",
				id: `inspection-tab-${r}`,
				role: "tab",
				"aria-controls": `inspection-${r}`,
				"aria-selected": e.tab === r,
				tabIndex: e.tab === r ? 0 : -1,
				"data-inspection-tab": r,
				onClick: () => t.selectTab(e, r),
				onKeyDown: (i) => {
					if (![
						"ArrowLeft",
						"ArrowRight",
						"Home",
						"End"
					].includes(i.key)) return;
					i.preventDefault();
					let a = n.indexOf(r), o = i.key === "Home" ? 0 : i.key === "End" ? 2 : (a + (i.key === "ArrowRight" ? 1 : 2)) % 3;
					t.selectTab(e, n[o], !0);
				},
				children: r[0].toUpperCase() + r.slice(1)
			}, r))
		}),
		/* @__PURE__ */ (0, D.jsx)("p", {
			className: "fine inspection-boundary",
			children: "Read-only inspection · no agent activation"
		}),
		/* @__PURE__ */ (0, D.jsx)("input", {
			type: "hidden",
			name: "csrf",
			value: i.csrf
		}),
		/* @__PURE__ */ (0, D.jsxs)("section", {
			id: "inspection-files",
			role: "tabpanel",
			"aria-labelledby": "inspection-tab-files",
			tabIndex: 0,
			hidden: e.tab !== "files",
			children: [
				/* @__PURE__ */ (0, D.jsxs)("div", {
					className: "inspection-toolbar",
					children: [/* @__PURE__ */ (0, D.jsx)("h2", { children: "Project files" }), /* @__PURE__ */ (0, D.jsxs)("button", {
						className: "quiet",
						type: "button",
						"data-inspection-refresh": "files",
						title: "Refresh from disk",
						onClick: () => void t.files(e, e.path),
						children: [/* @__PURE__ */ (0, D.jsx)(as, { kind: "refresh" }), /* @__PURE__ */ (0, D.jsx)("span", { children: "Refresh" })]
					})]
				}),
				/* @__PURE__ */ (0, D.jsxs)("div", {
					className: "inspection-toolbar inspection-location",
					children: [/* @__PURE__ */ (0, D.jsx)("button", {
						className: "quiet inspection-up",
						type: "button",
						"data-inspection-up": !0,
						"aria-label": "Open parent folder",
						title: "Open parent folder",
						disabled: e.filesPending || e.path === ".",
						onClick: () => void t.files(e, e.path.includes("/") ? e.path.slice(0, e.path.lastIndexOf("/")) : "."),
						children: /* @__PURE__ */ (0, D.jsx)(as, { kind: "up" })
					}), /* @__PURE__ */ (0, D.jsx)("nav", {
						className: "inspection-breadcrumbs",
						"data-inspection-path": !0,
						"aria-label": "File location",
						title: e.path,
						children: ["Project root", ...r].map((n, i) => {
							let a = i ? r.slice(0, i).join("/") : ".";
							return /* @__PURE__ */ (0, D.jsxs)(l.Fragment, { children: [i > 0 && /* @__PURE__ */ (0, D.jsx)("span", {
								className: "inspection-crumb-separator",
								children: "/"
							}), /* @__PURE__ */ (0, D.jsx)("button", {
								className: "inspection-crumb",
								type: "button",
								"data-inspection-crumb": a,
								title: a,
								"aria-current": i === r.length ? "location" : void 0,
								disabled: e.filesPending || i === r.length,
								onClick: () => void t.files(e, a),
								children: n
							})] }, a);
						})
					})]
				}),
				/* @__PURE__ */ (0, D.jsx)(ss, {
					view: e,
					kind: "files"
				}),
				/* @__PURE__ */ (0, D.jsx)("ul", {
					className: "inspection-list",
					"data-files-list": !0,
					"aria-label": "Project files",
					"aria-busy": e.filesPending,
					children: e.files.map((n) => /* @__PURE__ */ (0, D.jsx)("li", { children: /* @__PURE__ */ (0, D.jsxs)("button", {
						type: "button",
						title: `${n.name} · ${n.kind === "directory" ? "Folder" : "File"}`,
						"data-inspection-entry": n.path,
						"data-entry-kind": n.kind,
						"data-files-generation": a,
						disabled: e.filesPending || !e.filesFresh,
						"aria-current": e.fileSelected === n.path ? "true" : void 0,
						"aria-busy": e.fileBusy === n.path ? "true" : void 0,
						onClick: () => void t.file(e, n, a),
						children: [
							/* @__PURE__ */ (0, D.jsx)(as, { kind: n.kind === "directory" ? "folder" : "file" }),
							/* @__PURE__ */ (0, D.jsx)("span", {
								className: "inspection-entry-name",
								title: n.name,
								children: /* @__PURE__ */ (0, D.jsx)(os, { value: n.name })
							}),
							/* @__PURE__ */ (0, D.jsx)("span", {
								className: "inspection-entry-kind",
								children: n.kind === "directory" ? "Folder" : "File"
							}),
							n.kind === "directory" && /* @__PURE__ */ (0, D.jsx)(as, { kind: "chevron" })
						]
					}) }, n.path))
				}),
				/* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "button",
					"data-files-more": !0,
					hidden: !e.more,
					disabled: e.filesPending || !e.filesFresh,
					onClick: () => void t.files(e, e.path, e.next, !0),
					children: "Load more files"
				}),
				/* @__PURE__ */ (0, D.jsx)(cs, {
					view: e,
					kind: "file"
				})
			]
		}),
		/* @__PURE__ */ (0, D.jsxs)("section", {
			id: "inspection-changes",
			role: "tabpanel",
			"aria-labelledby": "inspection-tab-changes",
			tabIndex: 0,
			hidden: e.tab !== "changes",
			children: [
				/* @__PURE__ */ (0, D.jsxs)("div", {
					className: "inspection-toolbar",
					children: [/* @__PURE__ */ (0, D.jsx)("h2", { children: "Working changes" }), /* @__PURE__ */ (0, D.jsxs)("button", {
						className: "quiet",
						type: "button",
						"data-inspection-refresh": "changes",
						title: "Refresh from disk",
						onClick: () => void t.changes(e),
						children: [/* @__PURE__ */ (0, D.jsx)(as, { kind: "refresh" }), /* @__PURE__ */ (0, D.jsx)("span", { children: "Refresh" })]
					})]
				}),
				/* @__PURE__ */ (0, D.jsx)("p", {
					className: "fine",
					children: "Current files on disk, not a per-turn change log."
				}),
				/* @__PURE__ */ (0, D.jsx)(ss, {
					view: e,
					kind: "changes"
				}),
				/* @__PURE__ */ (0, D.jsx)("ul", {
					className: "inspection-list",
					"data-changes-list": !0,
					"aria-label": "Changed files",
					"aria-busy": e.changesPending,
					children: e.changes.map((n) => {
						let r = `${n.kind}:${n.path}`, i = n.kind[0].toUpperCase() + n.kind.slice(1) + (n.status ? ` · ${n.status}` : "");
						return /* @__PURE__ */ (0, D.jsx)("li", { children: /* @__PURE__ */ (0, D.jsxs)("button", {
							type: "button",
							title: `${n.path} · ${i}`,
							"data-inspection-change": n.path,
							"data-change-kind": n.kind,
							"data-changes-generation": o,
							disabled: e.changesPending || !e.changesFresh,
							"aria-current": e.diffSelected === r ? "true" : void 0,
							"aria-busy": e.diffBusy === r ? "true" : void 0,
							onClick: () => void t.diff(e, n, o),
							children: [
								/* @__PURE__ */ (0, D.jsx)(as, { kind: "file" }),
								/* @__PURE__ */ (0, D.jsx)("span", {
									className: "inspection-entry-name",
									title: n.path,
									children: /* @__PURE__ */ (0, D.jsx)(os, { value: n.path })
								}),
								/* @__PURE__ */ (0, D.jsx)("span", {
									className: "inspection-entry-kind",
									children: i
								})
							]
						}) }, r);
					})
				}),
				/* @__PURE__ */ (0, D.jsx)(cs, {
					view: e,
					kind: "diff"
				})
			]
		}),
		/* @__PURE__ */ (0, D.jsxs)("section", {
			id: "inspection-project",
			role: "tabpanel",
			"aria-labelledby": "inspection-tab-project",
			tabIndex: 0,
			hidden: e.tab !== "project",
			children: [
				/* @__PURE__ */ (0, D.jsx)("h2", {
					className: "inspection-project-name",
					children: i.project.name
				}),
				/* @__PURE__ */ (0, D.jsxs)("dl", { children: [
					/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Host folder" }), /* @__PURE__ */ (0, D.jsx)("dd", {
						className: "mono catalog-path",
						children: i.project.path
					})] }),
					/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Availability" }), /* @__PURE__ */ (0, D.jsx)("dd", { children: i.project.available ? "Folder available" : "Folder missing or identity changed" })] }),
					i.live ? /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Live session" }), /* @__PURE__ */ (0, D.jsx)("dd", {
						className: "mono catalog-path",
						children: i.live.session_id
					})] }), /* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Model" }), /* @__PURE__ */ (0, D.jsxs)("dd", { children: [
						i.live.provider,
						" / ",
						i.live.model
					] })] })] }) : /* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Saved history" }), /* @__PURE__ */ (0, D.jsx)("dd", { children: "Read-only, current branch" })] })
				] }),
				/* @__PURE__ */ (0, D.jsxs)("details", {
					className: "remove-project",
					children: [
						/* @__PURE__ */ (0, D.jsx)("summary", { children: "Remove project registration" }),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "Only the manager entry is removed. Project files and every saved session are retained."
						}),
						/* @__PURE__ */ (0, D.jsxs)("form", {
							method: "post",
							action: `/projects/${encodeURIComponent(e.project)}/remove`,
							children: [
								/* @__PURE__ */ (0, D.jsx)("input", {
									type: "hidden",
									name: "csrf",
									value: i.csrf
								}),
								/* @__PURE__ */ (0, D.jsxs)("label", {
									className: "checkbox-label",
									children: [/* @__PURE__ */ (0, D.jsx)("input", {
										type: "checkbox",
										name: "confirm",
										value: "remove",
										required: !0
									}), " Remove this registration; keep all files and sessions"]
								}),
								/* @__PURE__ */ (0, D.jsx)("button", {
									type: "submit",
									className: "button danger",
									children: "Remove from manager"
								})
							]
						})
					]
				})
			]
		}),
		/* @__PURE__ */ (0, D.jsx)("noscript", { children: /* @__PURE__ */ (0, D.jsx)("p", {
			className: "fine",
			children: "Files and Changes need JavaScript. Inspection never starts an agent."
		}) })
	] });
}
//#endregion
//#region src/inspection/controller.tsx
var us = null, ds = Object.freeze({
	files: "inspect/files",
	file: "inspect/file",
	changes: "inspect/changes",
	diff: "inspect/diff"
});
function fs(e) {
	return us === e && e.mount.isConnected && e.panel.contains(e.mount) && document.querySelector("#project-inspector") === e.panel && e.panel.dataset.project === e.project && String(e.props.project.available) === e.panel.dataset.available && (!e.owner || document.querySelector("#live-session") === e.owner && e.owner.dataset.project === e.project && (e.owner.dataset.instance || "") === e.instance && (e.owner.dataset.session || "") === e.session);
}
function ps(e) {
	return fs(e) ? !0 : (us === e && Ss(), !1);
}
function ms(e) {
	return ps(e) && !e.panel.hidden && document.visibilityState === "visible";
}
function hs(e) {
	fs(e) && (0, u.flushSync)(() => e.root.render(/* @__PURE__ */ (0, D.jsx)(ls, {
		view: e,
		actions: Ms
	})));
}
function gs(e, t, n, r = !1) {
	e.notices[t] = {
		status: r ? "" : n,
		error: r ? n : ""
	};
}
function _s(e, t) {
	e.requests[t]?.abort(), delete e.requests[t];
}
function vs(e, t) {
	e[t === "file" ? "fileGeneration" : "diffGeneration"]++, _s(e, t), t === "file" ? (e.file = null, e.fileBusy = "", e.fileSelected = "") : (e.diff = null, e.diffBusy = "", e.diffSelected = "");
}
function ys(e) {
	for (let t of [
		"files",
		"file",
		"changes",
		"diff"
	]) _s(e, t);
	e.filesGeneration++, e.changesGeneration++, e.filesPending = !1, e.changesPending = !1, e.fileGeneration++, e.diffGeneration++, e.fileBusy = "", e.diffBusy = "", hs(e);
}
function bs() {
	let e = document.querySelector("#project-inspector");
	if (us?.panel === e && fs(us)) return;
	Ss();
	let t = e?.querySelector("[data-react-inspection]");
	if (!e || !t) return;
	let n = null;
	try {
		let e = t.dataset.reactProps || "";
		e.length <= 32768 && (n = Yo(JSON.parse(e)));
	} catch {}
	if (!n || n.project.id !== e.dataset.project || String(n.project.available) !== e.dataset.available) return;
	let r = document.querySelector("#live-session"), i = {
		panel: e,
		mount: t,
		root: (0, d.createRoot)(t),
		props: n,
		project: n.project.id,
		tab: "files",
		path: ".",
		next: 0,
		owner: r,
		instance: r?.dataset.instance || "",
		session: r?.dataset.session || "",
		observer: new MutationObserver(() => {
			ps(i) && !ms(i) && ys(i);
		}),
		lifecycle: new AbortController(),
		requests: {},
		files: [],
		changes: [],
		filesPending: !1,
		changesPending: !1,
		filesLoaded: !1,
		changesLoaded: !1,
		filesFresh: !1,
		changesFresh: !1,
		filesGeneration: 0,
		changesGeneration: 0,
		more: !1,
		fileGeneration: 0,
		diffGeneration: 0,
		fileSelected: "",
		fileBusy: "",
		diffSelected: "",
		diffBusy: "",
		file: null,
		diff: null,
		notices: {
			files: {
				status: "Open Files to inspect this host project.",
				error: ""
			},
			changes: {
				status: "Open Changes to inspect Git changes.",
				error: ""
			}
		}
	};
	us = i, hs(i), i.observer.observe(e, {
		attributes: !0,
		attributeFilter: [
			"hidden",
			"data-project",
			"data-available"
		]
	}), r && i.observer.observe(r, {
		attributes: !0,
		attributeFilter: [
			"data-project",
			"data-instance",
			"data-session"
		]
	});
	let a = e.closest("#workspace");
	a?.parentElement && i.observer.observe(a.parentElement, { childList: !0 }), document.addEventListener("visibilitychange", () => {
		ps(i) && (ms(i) || ys(i));
	}, { signal: i.lifecycle.signal });
}
function xs(e) {
	let t = us;
	if (!t || !fs(t) || !t.owner?.isConnected) return !1;
	let n = Xo(e, t.props.project.id, {
		project_id: t.project,
		instance_id: t.instance,
		session_id: t.session
	});
	return n ? (t.props = {
		...t.props,
		live: n
	}, hs(t), !0) : !1;
}
function Ss() {
	let e = us;
	if (us = null, e) {
		e.observer.disconnect(), e.lifecycle.abort();
		for (let t of Object.values(e.requests)) t.abort();
		(0, u.flushSync)(() => e.root.unmount());
	}
}
async function Cs(e, t, n) {
	_s(e, t);
	let r = new AbortController();
	e.requests[t] = r;
	let i = setTimeout(() => r.abort(), 1e4), a = () => ms(e) && e.requests[t] === r && !r.signal.aborted;
	try {
		let i = await fetch(`/projects/${encodeURIComponent(e.project)}/${ds[t]}`, {
			method: "POST",
			credentials: "same-origin",
			cache: "no-store",
			redirect: "error",
			headers: {
				"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
				Accept: "application/json"
			},
			body: new URLSearchParams({
				csrf: e.props.csrf,
				...n
			}),
			signal: r.signal
		});
		if (!i.ok) throw Error("unavailable");
		let o = await i.text();
		if (r.signal.aborted && e.requests[t] === r) throw Error("timeout");
		if (!a()) return null;
		if (o.length > 3145728) throw Error("oversized");
		return JSON.parse(o);
	} catch {
		if (!ps(e) || e.requests[t] !== r) return null;
		throw Error(r.signal.aborted ? "Inspection timed out. Refresh to try again." : "Inspection unavailable. The path may be protected, unsupported, too large, or changed. Refresh to try again.");
	} finally {
		clearTimeout(i);
	}
}
function ws(e, t) {
	return !ms(e) || e.tab !== t ? !1 : e.panel.dataset.available === "true" || (gs(e, t, "Project folder is missing or its identity changed. Re-register the folder before inspecting it.", !0), hs(e), !1);
}
async function Ts(e, t = ".", n = 0, r = !1) {
	if (!ws(e, "files")) return;
	let i = ++e.filesGeneration;
	e.filesPending = !0, e.filesFresh = !1, vs(e, "file"), gs(e, "files", "Reading project files…"), hs(e);
	try {
		let a = await Cs(e, "files", {
			path: t,
			offset: String(n)
		});
		if (a === null || !ms(e) || e.filesGeneration !== i) return;
		if (!Zo(a)) throw Error("Unexpected file listing. Refresh to try again.");
		e.path = a.path, e.next = a.next_offset, e.filesLoaded = !0, e.filesFresh = !0, e.files = ns(e.files, a.entries, r), e.more = a.has_more && e.files.length < 4096, gs(e, "files", a.limited ? "Listing limit reached. Refresh or open a subfolder to inspect more." : e.files.length ? `${e.files.length} entries shown. Protected names, links and special files are omitted.` : "No visible files in this folder. Protected names, links and special files are omitted.");
	} catch (t) {
		fs(e) && e.filesGeneration === i && gs(e, "files", t.message + (e.files.length ? " Previously listed rows are stale until refresh succeeds." : ""), !0);
	} finally {
		if (fs(e) && e.filesGeneration === i) {
			e.filesPending = !1, hs(e);
			let t = e.panel.querySelector("[data-inspection-path]");
			t && (t.scrollLeft = t.scrollWidth);
		}
	}
}
async function Es(e) {
	if (!ws(e, "changes")) return;
	let t = ++e.changesGeneration;
	e.changesPending = !0, e.changesFresh = !1, vs(e, "diff"), gs(e, "changes", "Reading working changes…"), hs(e);
	try {
		let n = await Cs(e, "changes", {});
		if (n === null || !ms(e) || e.changesGeneration !== t) return;
		if (!$o(n)) throw Error("Unexpected changes response. Refresh to try again.");
		e.changesLoaded = !0, e.changesFresh = !0, e.changes = n.available ? n.changes : [], gs(e, "changes", n.available ? n.limited ? "Change listing truncated. Only a bounded set is shown." : e.changes.length ? `${e.changes.length} changes shown. Select a file for a read-only diff.` : "No working changes reported." : n.reason || "Git changes are unavailable for this project.");
	} catch (n) {
		fs(e) && e.changesGeneration === t && gs(e, "changes", n.message + (e.changes.length ? " Previously listed rows are stale until refresh succeeds." : ""), !0);
	} finally {
		fs(e) && e.changesGeneration === t && (e.changesPending = !1, hs(e));
	}
}
async function Ds(e, t, n) {
	if (!ws(e, "files") || e.filesPending || !e.filesFresh || e.filesGeneration !== n || !e.files.includes(t)) return;
	if (t.kind === "directory") {
		Ts(e, t.path);
		return;
	}
	vs(e, "file"), e.fileBusy = t.path, gs(e, "files", "Reading text preview…"), hs(e);
	let r = e.fileGeneration, i = () => fs(e) && e.fileGeneration === r && e.filesGeneration === n && e.tab === "files" && e.fileBusy === t.path;
	try {
		let n = await Cs(e, "file", { path: t.path });
		if (n === null || !i()) return;
		if (!Qo(n) || n.path !== t.path) throw Error("Unexpected file preview. Refresh to try again.");
		e.file = {
			path: n.path,
			text: n.text,
			notice: n.truncated ? "Preview truncated to 64 KiB. The original file is unchanged." : n.text ? "Read-only UTF-8 text preview." : "This file is empty."
		}, e.fileSelected = t.path, gs(e, "files", "Preview loaded. Files on disk are unchanged.");
	} catch (t) {
		i() && gs(e, "files", t.message, !0);
	} finally {
		i() && (e.fileBusy = "", hs(e));
	}
}
async function Os(e, t, n) {
	if (!ws(e, "changes") || e.changesPending || !e.changesFresh || e.changesGeneration !== n || !e.changes.includes(t)) return;
	let r = `${t.kind}:${t.path}`;
	vs(e, "diff"), e.diffBusy = r, gs(e, "changes", "Reading diff preview…"), hs(e);
	let i = e.diffGeneration, a = () => fs(e) && e.diffGeneration === i && e.changesGeneration === n && e.tab === "changes" && e.diffBusy === r;
	try {
		let n = await Cs(e, "diff", {
			path: t.path,
			kind: t.kind
		});
		if (n === null || !a()) return;
		if (!es(n)) throw Error("Unexpected diff response. Refresh to try again.");
		if (!n.available) {
			gs(e, "changes", n.reason || "A diff is unavailable for this file.");
			return;
		}
		if (n.path !== t.path || n.kind !== t.kind) throw Error("Unexpected diff response. Refresh to try again.");
		e.diff = {
			path: n.path,
			text: n.text,
			notice: n.truncated ? "Diff truncated for bounded display." : n.text ? `Read-only diff · ${t.kind}` : "No text diff is available for this file."
		}, e.diffSelected = r, gs(e, "changes", "Diff loaded. No files or Git state were changed.");
	} catch (t) {
		a() && gs(e, "changes", t.message, !0);
	} finally {
		a() && (e.diffBusy = "", hs(e));
	}
}
function ks(e, t, n = !1) {
	ps(e) && (e.tab !== t && (e.tab === "files" && (vs(e, "file"), _s(e, "files"), e.filesGeneration++, e.filesPending = !1, /^(Reading text preview|Preview loaded)/.test(e.notices.files.status) && gs(e, "files", "Select a file for a read-only preview.")), e.tab === "changes" && (vs(e, "diff"), _s(e, "changes"), e.changesGeneration++, e.changesPending = !1, /^(Reading diff preview|Diff loaded)/.test(e.notices.changes.status) && gs(e, "changes", "Select a file for a read-only diff."))), e.tab = t, hs(e), n && e.panel.querySelector(`[data-inspection-tab="${t}"]`)?.focus(), ms(e) && t !== "project" && ws(e, t) && (t === "files" && !e.filesLoaded && !e.filesPending && Ts(e), t === "changes" && !e.changesLoaded && !e.changesPending && Es(e)));
}
function As() {
	us && ks(us, us.tab);
}
function js(e, t) {
	return !us || !ps(us) || us.project !== t || ![
		"files",
		"changes",
		"project"
	].includes(e) ? !1 : (ks(us, e), !0);
}
var Ms = {
	files: Ts,
	changes: Es,
	file: Ds,
	diff: Os,
	selectTab: ks
}, Ns = Object.freeze({
	init: bs,
	opened: As,
	select: js,
	refresh: xs,
	dispose: Ss
});
//#endregion
//#region src/processes/Panel.tsx
function Ps({ view: e, actions: t }) {
	let n = e.records.find((t) => t.process_id === e.confirmation);
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("button", {
		type: "button",
		className: "quiet",
		"data-processes-open": !0,
		"aria-haspopup": "dialog",
		"aria-controls": "processes-dialog",
		children: "Managed processes"
	}), /* @__PURE__ */ (0, D.jsxs)("dialog", {
		ref: e.dialog,
		id: "processes-dialog",
		className: "runtime-dialog processes-dialog",
		"aria-labelledby": "processes-heading",
		onClose: () => t.toggle(e),
		onCancel: (n) => {
			e.confirmation && (n.preventDefault(), t.cancel(e));
		},
		children: [/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "dialog-heading",
			children: [/* @__PURE__ */ (0, D.jsx)("h2", {
				id: "processes-heading",
				children: "Managed processes"
			}), /* @__PURE__ */ (0, D.jsx)("button", {
				type: "button",
				className: "quiet icon-button",
				"data-processes-close": !0,
				"aria-label": "Close managed processes",
				title: "Close",
				children: /* @__PURE__ */ (0, D.jsx)("svg", {
					viewBox: "0 0 24 24",
					fill: "none",
					stroke: "currentColor",
					strokeWidth: "1.5",
					"aria-hidden": "true",
					children: /* @__PURE__ */ (0, D.jsx)("path", { d: "m6 6 12 12M18 6 6 18" })
				})
			})]
		}), /* @__PURE__ */ (0, D.jsx)("div", {
			className: "runtime-dialog-body",
			children: /* @__PURE__ */ (0, D.jsxs)("details", {
				ref: e.panel,
				id: "managed-processes",
				className: "managed-processes",
				"data-process-project": e.project,
				"data-process-instance": e.instance,
				"data-process-session": e.session,
				onToggle: () => t.toggle(e),
				children: [/* @__PURE__ */ (0, D.jsx)("summary", { children: "Managed processes" }), /* @__PURE__ */ (0, D.jsxs)("div", {
					className: "managed-process-body",
					children: [
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "Only this live conversation’s managed processes. No commands can be launched here. Logs may contain untrusted application output. Stop requires Default mode and current process permission."
						}),
						/* @__PURE__ */ (0, D.jsxs)("div", {
							className: "process-toolbar",
							children: [/* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "quiet",
								"data-process-refresh": !0,
								disabled: e.busy || !!n,
								onClick: () => void t.refresh(e),
								children: "Refresh"
							}), /* @__PURE__ */ (0, D.jsx)("span", {
								"data-process-status": !0,
								role: "status",
								children: e.status
							})]
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "error",
							"data-process-error": !0,
							role: "alert",
							hidden: !e.error,
							children: e.error
						}),
						/* @__PURE__ */ (0, D.jsx)("ul", {
							"data-process-list": !0,
							"aria-label": "Current session managed processes",
							children: e.records.map((r) => /* @__PURE__ */ (0, D.jsxs)("li", {
								className: "process-row",
								children: [
									/* @__PURE__ */ (0, D.jsxs)("span", {
										className: "process-identity",
										children: [/* @__PURE__ */ (0, D.jsxs)("span", { children: [
											r.name,
											" · ",
											r.status,
											r.ready ? " · ready" : ""
										] }), /* @__PURE__ */ (0, D.jsx)("small", { children: r.process_id })]
									}),
									/* @__PURE__ */ (0, D.jsx)("button", {
										type: "button",
										className: "quiet",
										"data-process-logs": r.process_id,
										disabled: e.busy || !!n,
										onClick: () => t.select(e, r.process_id),
										children: "Logs"
									}),
									/* @__PURE__ */ (0, D.jsx)("button", {
										type: "button",
										className: "quiet",
										"data-process-stop": r.process_id,
										disabled: e.busy || e.unknown || !!n || r.status !== "running",
										onClick: (n) => t.prepare(e, r.process_id, n.currentTarget),
										children: "Stop"
									})
								]
							}, r.process_id))
						}),
						n && /* @__PURE__ */ (0, D.jsxs)("section", {
							className: "process-stop-confirmation",
							role: "group",
							"aria-labelledby": "process-stop-heading",
							children: [
								/* @__PURE__ */ (0, D.jsx)("h3", {
									id: "process-stop-heading",
									children: "Stop managed process?"
								}),
								/* @__PURE__ */ (0, D.jsxs)("p", { children: [
									"Stop managed process ",
									n.name,
									" (",
									n.process_id,
									") in this conversation?"
								] }),
								/* @__PURE__ */ (0, D.jsx)("p", {
									className: "fine",
									children: "Requires Default mode and current process permission. This does not stop the conversation."
								}),
								/* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "quiet",
									"data-process-stop-cancel": !0,
									onClick: () => t.cancel(e),
									children: "Cancel"
								}),
								/* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "button danger",
									"data-process-stop-confirm": !0,
									onClick: () => void t.stop(e),
									children: "Stop process"
								})
							]
						}),
						/* @__PURE__ */ (0, D.jsxs)("section", {
							"data-process-log-panel": !0,
							hidden: !e.logHeading,
							"aria-label": "Managed process log",
							children: [
								/* @__PURE__ */ (0, D.jsx)("h3", {
									"data-process-log-heading": !0,
									children: e.logHeading
								}),
								/* @__PURE__ */ (0, D.jsx)("p", {
									"data-process-log-status": !0,
									className: "fine",
									children: e.logStatus
								}),
								/* @__PURE__ */ (0, D.jsx)("pre", {
									"data-process-output": !0,
									tabIndex: 0,
									"aria-label": "Bounded plain-text process output",
									children: e.output
								}),
								/* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "quiet",
									"data-process-log-more": !0,
									disabled: e.busy || e.eof || !!n || !e.logHeading,
									onClick: () => void t.logs(e),
									children: "Read next output"
								})
							]
						})
					]
				})]
			})
		})]
	})] });
}
//#endregion
//#region src/processes/model.ts
var Fs = (e) => !!e && typeof e == "object" && !Array.isArray(e), Is = (e) => new TextEncoder().encode(e).length, Ls = (e) => typeof e == "string" && /^proc_[a-f0-9]{32}$/.test(e), Rs = (e) => e === "running" || e === "stopped" || e === "exited";
function zs(e) {
	return Fs(e) && Ls(e.process_id) && typeof e.name == "string" && Is(e.name) <= 64 && Rs(e.status) && (e.ready === void 0 || typeof e.ready == "boolean");
}
function Bs(e) {
	return !Fs(e) || !Array.isArray(e.processes) || e.processes.length > 128 || typeof e.truncated != "boolean" || !e.processes.every(zs) || new Set(e.processes.map((e) => e.process_id)).size !== e.processes.length ? null : {
		processes: e.processes,
		truncated: e.truncated
	};
}
function Vs(e, t, n = 0) {
	return !Fs(e) || e.process_id !== t || !Rs(e.status) || typeof e.output != "string" || Is(e.output) > 32768 || !Number.isSafeInteger(e.next_cursor) || e.next_cursor < n || !Number.isSafeInteger(e.omitted_bytes) || e.omitted_bytes < 0 || typeof e.eof != "boolean" ? null : {
		output: e.output.replace(/\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)/g, "").replace(/[\x00-\x08\x0b-\x1f\x7f-\x9f]/g, ""),
		next_cursor: e.next_cursor,
		omitted_bytes: e.omitted_bytes,
		eof: e.eof
	};
}
function Hs(e, t) {
	return Fs(e) && e.project_id === t.project && e.instance_id === t.instance && Fs(e.result) && e.result.session_id === t.session ? e.result : null;
}
//#endregion
//#region src/processes/controller.tsx
var Us = null;
function Ws(e) {
	return Us === e && e.mount.isConnected && e.owner === document.querySelector("#live-session") && e.owner.dataset.project === e.project && e.owner.dataset.instance === e.instance && e.owner.dataset.session === e.session;
}
function Gs(e) {
	return Ws(e) ? !0 : (Us === e && Ys(), !1);
}
function Ks(e) {
	return Gs(e) && !!e.panel.current?.open && !!e.dialog.current?.open && document.visibilityState === "visible" && e.panel.current.getClientRects().length > 0;
}
function qs(e) {
	Ws(e) && (0, u.flushSync)(() => e.root.render(/* @__PURE__ */ (0, D.jsx)(Ps, {
		view: e,
		actions: rc
	})));
}
function Js() {
	Ys();
	let e = document.querySelector("[data-react-processes]"), t = document.querySelector("#live-session");
	if (!e || !t || !t.contains(e) || !t.dataset.project || !t.dataset.instance || !t.dataset.session) return;
	let n = new AbortController(), r = {
		mount: e,
		owner: t,
		root: (0, d.createRoot)(e),
		project: t.dataset.project,
		instance: t.dataset.instance,
		session: t.dataset.session,
		controller: n,
		observer: new MutationObserver(() => {
			Gs(r) && !Ks(r) && Xs(r);
		}),
		panel: (0, l.createRef)(),
		dialog: (0, l.createRef)(),
		generation: 0,
		records: [],
		selected: "",
		eof: !1,
		output: "",
		logHeading: "",
		logStatus: "",
		status: "Open to read inventory.",
		error: "",
		busy: !1,
		unknown: !1,
		confirmation: "",
		confirmationReturn: null,
		request: null
	};
	Us = r, qs(r), r.observer.observe(t, {
		attributes: !0,
		attributeFilter: [
			"data-project",
			"data-instance",
			"data-session",
			"hidden"
		]
	});
	let i = t.closest("#workspace");
	i?.parentElement && r.observer.observe(i.parentElement, { childList: !0 }), document.addEventListener("visibilitychange", () => {
		Gs(r) && (Ks(r) ? $s(r) : Xs(r));
	}, { signal: n.signal });
}
function Ys() {
	let e = Us;
	Us = null, e && (clearTimeout(e.timer), e.observer.disconnect(), e.controller.abort(), e.request?.controller.abort(), e.panel.current && (e.panel.current.open = !1), e.dialog.current?.open && e.dialog.current.close(), (0, u.flushSync)(() => e.root.unmount()));
}
function Xs(e) {
	clearTimeout(e.timer), e.request && e.request.action !== "stop" && (e.generation++, e.request.controller.abort(), e.request = null, e.busy = !1), e.confirmation = "", qs(e);
}
function Zs(e) {
	clearTimeout(e.timer), Ks(e) && !e.unknown && !e.confirmation && (e.timer = setTimeout(() => void $s(e), 3e3));
}
async function Qs(e, t, n = {}) {
	if (!Gs(e)) throw Error("stale");
	let r = {
		controller: new AbortController(),
		action: t
	};
	e.request = r;
	let i = AbortSignal.any([
		e.controller.signal,
		r.controller.signal,
		AbortSignal.timeout(12e3)
	]), a = document.querySelector("input[name=\"csrf\"]")?.value || "", o = await fetch(`/projects/${encodeURIComponent(e.project)}/processes/${t}`, {
		method: "POST",
		credentials: "same-origin",
		redirect: "error",
		cache: "no-store",
		signal: i,
		headers: {
			"Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
			Accept: "application/json"
		},
		body: new URLSearchParams({
			csrf: a,
			instance_id: e.instance,
			session_id: e.session,
			...n
		})
	});
	if (!o.ok) throw Error("rejected");
	let s = await o.text();
	if (i.aborted || !Gs(e) || e.request !== r) throw Error("stale");
	if (s.length > 262144) throw Error("oversized");
	let c = Hs(JSON.parse(s), e);
	if (!c) throw Error("stale");
	return c;
}
async function $s(e) {
	if (!Ks(e) || e.busy || e.confirmation) return;
	e.busy = !0;
	let t = ++e.generation;
	try {
		let t = Bs(await Qs(e, "list"));
		if (!t) throw Error("invalid");
		if (!Ks(e)) return;
		e.records = t.processes, e.status = `${e.records.length} managed process${e.records.length === 1 ? "" : "es"}${t.truncated ? " · inventory truncated" : ""}`, e.selected && !e.records.some((t) => t.process_id === e.selected) && ec(e), e.unknown || (e.error = "");
	} catch {
		Ws(e) && e.generation === t && e.request && Ks(e) && (e.error = "Inventory unavailable. Review the current live conversation; no work was started.");
	} finally {
		Ws(e) && e.generation === t && e.request?.action === "list" && (e.request = null, e.busy = !1, qs(e), Zs(e));
	}
}
function ec(e) {
	e.selected = "", e.cursor = void 0, e.eof = !1, e.output = "", e.logHeading = "", e.logStatus = "";
}
async function tc(e) {
	if (!Ks(e) || e.busy || e.eof || e.confirmation || !e.records.some((t) => t.process_id === e.selected)) return;
	e.busy = !0;
	let t = e.selected, n = ++e.generation;
	qs(e);
	try {
		let n = Vs(await Qs(e, "logs", {
			process_id: t,
			max_bytes: "32768",
			...e.cursor === void 0 ? {} : { cursor: String(e.cursor) }
		}), t, e.cursor);
		if (!n) throw Error("invalid");
		if (!Ks(e)) return;
		e.cursor = n.next_cursor, e.eof = n.eof, e.output = n.output, e.logHeading = `${e.records.find((e) => e.process_id === t)?.name} · output`, e.logStatus = `Cursor ${e.cursor} · ${n.omitted_bytes} bytes omitted${e.eof ? " · end of output" : " · more may arrive"}`;
	} catch {
		Ws(e) && e.generation === n && e.request && Ks(e) && (e.error = "Output unavailable. Select Logs again to read a fresh bounded page.");
	} finally {
		Ws(e) && e.generation === n && e.request?.action === "logs" && (e.request = null, e.busy = !1, qs(e), Zs(e));
	}
}
async function nc(e) {
	let t = e.confirmation;
	if (Ks(e) && !e.busy && !e.unknown && e.records.some((e) => e.process_id === t && e.status === "running")) {
		e.confirmation = "", e.busy = !0, clearTimeout(e.timer), qs(e);
		try {
			let n = await Qs(e, "stop", {
				process_id: t,
				grace_ms: "2000"
			});
			if (!Gs(e)) return;
			if (!zs(n.process) || n.process.process_id !== t) throw Error("invalid");
			let r = n.process;
			e.records = e.records.map((e) => e.process_id === t ? r : e), e.error = "";
		} catch {
			Ws(e) && (e.unknown = !0, e.error = "Stop was not acknowledged; it may have succeeded or been denied by policy. No retry was queued. Refresh inventory to inspect; reopen this panel’s workspace before issuing another Stop.");
		} finally {
			Ws(e) && (e.request = null, e.busy = !1, qs(e), e.dialog.current?.querySelector("[data-processes-close]")?.focus(), Zs(e));
		}
	}
}
var rc = {
	refresh: $s,
	logs: tc,
	stop: nc,
	toggle(e) {
		Gs(e) && (clearTimeout(e.timer), Ks(e) ? $s(e) : Xs(e));
	},
	select(e, t) {
		Ks(e) && !e.busy && !e.confirmation && e.records.some((e) => e.process_id === t) && (ec(e), e.selected = t, tc(e));
	},
	prepare(e, t, n) {
		Ks(e) && !e.busy && !e.unknown && e.records.some((e) => e.process_id === t && e.status === "running") && (clearTimeout(e.timer), e.confirmation = t, e.confirmationReturn = n, qs(e), e.dialog.current?.querySelector("[data-process-stop-cancel]")?.focus());
	},
	cancel(e) {
		Gs(e) && (e.confirmation = "", qs(e), e.confirmationReturn?.isConnected && e.confirmationReturn.focus(), Zs(e));
	}
}, ic = Object.freeze({
	init: Js,
	dispose: Ys
}), ac = new TextEncoder();
function oc(e) {
	return !!e && typeof e == "object" && !Array.isArray(e);
}
function sc(e, t) {
	return typeof e == "string" && !e.includes("\0") && !/[\uD800-\uDFFF]/u.test(e) && ac.encode(e).length <= t;
}
function cc(e) {
	return sc(e, 128) && /^[a-zA-Z0-9_-]+$/.test(e);
}
function lc(e) {
	if (!oc(e) || !cc(e.id) || !Array.isArray(e.questions) || !e.questions.length || e.questions.length > 16) return !1;
	let t = /* @__PURE__ */ new Set(), n = 0;
	for (let r of e.questions) {
		if (!oc(r) || !cc(r.id) || t.has(r.id) || !sc(r.question, 8192) || r.header != null && !sc(r.header, 256) || r.choices_only != null && typeof r.choices_only != "boolean" || r.options != null && !Array.isArray(r.options)) return !1;
		t.add(r.id);
		let e = r.options || [];
		if (e.length > 32 || r.choices_only && !e.length) return !1;
		n += ac.encode(r.question).length + ac.encode(r.header || "").length;
		for (let t of e) {
			if (!oc(t) || !sc(t.label, 512) || !t.label.trim() || t.description != null && !sc(t.description, 2048)) return !1;
			n += ac.encode(t.label).length + ac.encode(t.description || "").length;
		}
	}
	return n <= 65536;
}
function uc(e, t) {
	return typeof t?.selected == "number" ? e.options?.[t.selected]?.label || "" : e.choices_only ? "" : t?.custom || "";
}
function dc(e, t, n) {
	let r = 0;
	for (let i = 0; i < e.length; i++) {
		if (!n && i !== t.page) continue;
		let a = uc(e[i], t.answers[e[i].id]);
		if (r += ac.encode(a).length, !sc(a, 65536) || !a.trim() || r > 65536) return i;
	}
	return -1;
}
function fc(e) {
	if (!oc(e) || !cc(e.id) || e.truncated) return !0;
	for (let t of [
		"tool",
		"risk",
		"reason",
		"scope_label"
	]) if (e[t] != null && !sc(e[t], 8192)) return !0;
	for (let t of ["paths", "capabilities"]) {
		let n = e[t];
		if (n != null && (!Array.isArray(n) || n.length > 64 || !n.every((e) => sc(e, 8192)))) return !0;
	}
	let t = e.effects;
	return t != null && (!Array.isArray(t) || t.length > 64 || !t.every((e) => oc(e) && [
		"type",
		"capability",
		"operation",
		"resource",
		"command",
		"reason"
	].every((t) => e[t] == null || sc(e[t], 8192))));
}
function pc(e, t, n = "") {
	return e === "input" && lc(t) ? JSON.stringify([
		n,
		e,
		t.id,
		t.questions.map((e) => [
			e.id,
			e.header || "",
			e.question,
			!!e.choices_only,
			(e.options || []).map((e) => [e.label, e.description || ""])
		])
	]) : e === "permission" && oc(t) ? JSON.stringify([
		n,
		e,
		t.id,
		t.tool,
		t.risk,
		t.reason,
		t.scope_label,
		t.paths,
		t.capabilities,
		t.unknown,
		t.truncated,
		Array.isArray(t.effects) ? t.effects.map((e) => oc(e) ? [
			e.type,
			e.capability,
			e.operation,
			e.resource,
			e.command,
			e.reason
		] : null) : null
	]) : JSON.stringify([
		n,
		e,
		oc(t) ? t.id : null,
		"invalid"
	]);
}
//#endregion
//#region src/attention/Panel.tsx
var mc = /\s*(?:\((?:recommended|推荐)\)|（(?:recommended|推荐)）)\s*$/i;
function hc({ state: e }) {
	let t = e.stopping ? "Stopping…" : "Stop turn";
	return /* @__PURE__ */ (0, D.jsx)("button", {
		type: "button",
		className: "attention-stop quiet",
		"data-runtime-abort": "",
		title: "Stop the current turn without answering",
		disabled: !e.canStop,
		"aria-label": t,
		children: t
	});
}
function gc({ pending: e, state: t, onGuard: n }) {
	let r = oc(e) ? e : {}, i = fc(e), a = !t.safe || !cc(r.id), o = (e) => typeof e == "string" ? e : "", s = (e) => Array.isArray(e) ? e.slice(0, 64) : [];
	return /* @__PURE__ */ (0, D.jsxs)("section", {
		className: "attention-card attention-permission",
		"aria-busy": !!t.busy,
		onClickCapture: n,
		children: [
			/* @__PURE__ */ (0, D.jsx)("div", {
				className: "attention-warning",
				children: i ? "Incomplete summary · approval blocked" : "APPROVAL REQUIRED"
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "attention-body attention-permission-body",
				tabIndex: 0,
				role: "group",
				"aria-label": "Permission details",
				children: [
					/* @__PURE__ */ (0, D.jsxs)("h2", {
						className: "attention-title",
						children: [
							o(r.tool) || "Tool",
							" · ",
							o(r.risk) || "unspecified risk"
						]
					}),
					r.reason && /* @__PURE__ */ (0, D.jsx)("p", { children: o(r.reason) }),
					r.scope_label && /* @__PURE__ */ (0, D.jsx)("p", { children: o(r.scope_label) }),
					s(r.paths).map((e, t) => /* @__PURE__ */ (0, D.jsx)("pre", { children: o(e) }, t)),
					!!s(r.capabilities).length && /* @__PURE__ */ (0, D.jsxs)("p", { children: ["Capabilities: ", s(r.capabilities).map(o).join(", ")] }),
					s(r.effects).map((e, t) => /* @__PURE__ */ (0, D.jsx)("pre", { children: oc(e) ? [
						e.type,
						e.capability,
						e.operation,
						e.resource,
						e.command,
						e.reason
					].map(o).filter(Boolean).join(" · ") : "" }, t)),
					i && /* @__PURE__ */ (0, D.jsx)("p", {
						className: "attention-authority-warning",
						children: "This summary is incomplete and cannot be approved. Reject or stop the turn."
					}),
					(r.unknown || i) && /* @__PURE__ */ (0, D.jsx)("p", {
						className: "attention-authority-warning",
						children: "Some effects are unknown or omitted."
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "attention-authority",
				children: "Grants host authority, not sandboxed execution."
			}),
			/* @__PURE__ */ (0, D.jsxs)("footer", {
				className: "attention-footer attention-permission-actions",
				children: [
					/* @__PURE__ */ (0, D.jsx)(hc, { state: t }),
					/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "quiet danger",
						"data-permission": "deny",
						"data-request-id": o(r.id),
						disabled: a,
						children: "Reject"
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "primary",
						"data-permission": "allow",
						"data-request-id": o(r.id),
						disabled: a || i,
						"data-blocked": i ? "true" : void 0,
						title: i ? "Incomplete permission summaries cannot be approved. Reject or stop this turn." : void 0,
						children: "Allow once"
					})
				]
			})
		]
	});
}
function _c(e) {
	if (e.kind === "permission") return /* @__PURE__ */ (0, D.jsx)(gc, { ...e });
	let { pending: t, draft: n, state: r, feedback: i, formRef: a, bodyRef: o, onPage: s, onCollapse: c, onContinue: l, onAnswer: u, onSubmit: d, onGuard: f, onKeydown: p, onComposition: m } = e, h = lc(t), g = h ? t.questions : [], _ = !r.safe, v = n.page === g.length - 1;
	return /* @__PURE__ */ (0, D.jsxs)("form", {
		ref: a,
		className: "attention-card attention-questions" + (n.collapsed ? " attention-collapsed" : ""),
		"data-runtime-input": "",
		"data-request-id": oc(t) && typeof t.id == "string" ? t.id : "",
		noValidate: !0,
		"aria-busy": !!r.busy,
		onSubmitCapture: d,
		onClickCapture: f,
		children: [
			/* @__PURE__ */ (0, D.jsxs)("header", {
				className: "attention-header",
				children: [/* @__PURE__ */ (0, D.jsxs)("div", {
					className: "attention-heading",
					children: [/* @__PURE__ */ (0, D.jsx)("div", {
						className: "attention-eyebrow",
						children: "YOUR INPUT NEEDED"
					}), /* @__PURE__ */ (0, D.jsx)("h2", {
						className: "attention-title",
						id: "attention-question-title",
						children: h ? g[n.page]?.header || `Question ${n.page + 1}` : "Question unavailable"
					})]
				}), /* @__PURE__ */ (0, D.jsxs)("div", {
					className: "attention-header-actions",
					children: [/* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "attention-icon quiet",
						"data-attention-collapse": "",
						disabled: _,
						onClick: c,
						"aria-controls": "attention-question-body attention-question-footer",
						"aria-expanded": !n.collapsed,
						"aria-label": n.collapsed ? "Expand questions" : "Collapse questions",
						children: n.collapsed ? "+" : "−"
					}), /* @__PURE__ */ (0, D.jsx)(hc, { state: r })]
				})]
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				ref: o,
				className: "attention-body",
				id: "attention-question-body",
				hidden: n.collapsed,
				children: [
					/* @__PURE__ */ (0, D.jsx)("p", {
						className: "attention-feedback",
						id: "attention-feedback",
						role: "status",
						hidden: !i,
						children: i
					}),
					!h && /* @__PURE__ */ (0, D.jsx)("p", {
						className: "attention-detail",
						children: "This question batch is incomplete or unsupported. Stop the turn or review the workspace; no answers can be sent."
					}),
					g.map((e, t) => {
						let r = e.options || [], i = n.answers[e.id] || {
							selected: null,
							custom: ""
						}, a = i.selected === "custom" || i.selected === null;
						return /* @__PURE__ */ (0, D.jsxs)("fieldset", {
							className: "attention-question",
							"data-question-id": e.id,
							hidden: t !== n.page,
							disabled: _,
							"aria-describedby": `attention-detail-${t}`,
							children: [
								/* @__PURE__ */ (0, D.jsx)("legend", {
									className: "sr-only",
									children: e.header || `Question ${t + 1}`
								}),
								/* @__PURE__ */ (0, D.jsx)("p", {
									className: "attention-detail",
									id: `attention-detail-${t}`,
									children: e.question
								}),
								/* @__PURE__ */ (0, D.jsxs)("div", {
									className: "attention-options",
									children: [r.map((n, r) => /* @__PURE__ */ (0, D.jsxs)("label", {
										className: "attention-option checkbox-label",
										children: [
											/* @__PURE__ */ (0, D.jsx)("input", {
												className: "attention-radio",
												type: "radio",
												name: `question-${t}`,
												id: `question-${t}-option-${r}`,
												value: n.label,
												"data-option-index": r,
												checked: i.selected === r,
												disabled: _,
												onChange: () => u(e.id, { selected: r }, !0)
											}),
											/* @__PURE__ */ (0, D.jsx)("span", {
												className: "attention-number",
												"aria-hidden": "true",
												children: r + 1
											}),
											/* @__PURE__ */ (0, D.jsxs)("span", {
												className: "attention-option-copy",
												children: [/* @__PURE__ */ (0, D.jsxs)("span", {
													className: "attention-option-line",
													children: [/* @__PURE__ */ (0, D.jsx)("span", {
														className: "attention-option-label",
														children: n.label.replace(mc, "")
													}), mc.test(n.label) && /* @__PURE__ */ (0, D.jsx)("span", {
														className: "attention-badge",
														children: "Recommended"
													})]
												}), n.description && /* @__PURE__ */ (0, D.jsx)("span", {
													className: "attention-description",
													children: n.description
												})]
											})
										]
									}, r)), !e.choices_only && /* @__PURE__ */ (0, D.jsxs)("div", {
										className: "attention-custom" + (r.length ? "" : " attention-custom-block") + (a ? " attention-custom-active" : ""),
										children: [
											!!r.length && /* @__PURE__ */ (0, D.jsx)("input", {
												className: "attention-radio",
												type: "radio",
												name: `question-${t}`,
												value: "",
												"data-other": "true",
												checked: i.selected === "custom",
												"aria-label": "Other: write your own answer",
												disabled: _,
												onChange: () => u(e.id, { selected: "custom" }, !1, !0)
											}),
											/* @__PURE__ */ (0, D.jsxs)("label", {
												className: "attention-custom-label",
												htmlFor: `question-${t}-answer`,
												title: "Your answer",
												children: [!!r.length && /* @__PURE__ */ (0, D.jsx)("span", {
													className: "attention-number",
													"aria-hidden": "true",
													children: r.length + 1
												}), /* @__PURE__ */ (0, D.jsx)("span", {
													className: "sr-only",
													children: "Your answer"
												})]
											}),
											/* @__PURE__ */ (0, D.jsxs)("div", {
												className: "attention-answer-field",
												children: [/* @__PURE__ */ (0, D.jsx)("div", {
													className: "attention-answer-mirror",
													"aria-hidden": "true",
													children: i.custom + "\n"
												}), /* @__PURE__ */ (0, D.jsx)("textarea", {
													className: "attention-answer",
													id: `question-${t}-answer`,
													rows: 1,
													maxLength: 8192,
													disabled: _,
													placeholder: r.length ? "Other: write your own answer…" : "Write your answer…",
													value: i.custom,
													"aria-describedby": `attention-detail-${t}`,
													onChange: () => {},
													onInput: (t) => u(e.id, {
														selected: "custom",
														custom: t.currentTarget.value
													}),
													onFocus: () => u(e.id, { selected: "custom" }),
													onKeyDown: p,
													onCompositionStart: () => m(!0),
													onCompositionEnd: () => m(!1)
												})]
											})
										]
									})]
								})
							]
						}, e.id);
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsxs)("footer", {
				className: "attention-footer",
				id: "attention-question-footer",
				hidden: n.collapsed,
				children: [/* @__PURE__ */ (0, D.jsxs)("div", {
					className: "attention-pager",
					children: [
						/* @__PURE__ */ (0, D.jsx)("button", {
							type: "button",
							className: "attention-icon quiet",
							"data-attention-page": "-1",
							"aria-label": "Previous question",
							disabled: _ || !h || n.page === 0,
							onClick: () => s(-1),
							children: "‹"
						}),
						/* @__PURE__ */ (0, D.jsx)("span", {
							className: "attention-progress",
							"aria-live": "polite",
							children: h ? `${n.page + 1} / ${g.length}` : ""
						}),
						/* @__PURE__ */ (0, D.jsx)("button", {
							type: "button",
							className: "attention-icon quiet",
							"data-attention-page": "1",
							"aria-label": "Next question",
							disabled: _ || !h || v,
							onClick: () => s(1),
							children: "›"
						})
					]
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: v ? "submit" : "button",
					className: "attention-continue primary",
					"data-attention-continue": "",
					disabled: _ || !h,
					onClick: v ? void 0 : l,
					children: v ? "Submit answers" : "Next"
				})]
			})
		]
	});
}
//#endregion
//#region src/attention/controller.tsx
var vc = /* @__PURE__ */ new Map(), yc = 8, bc = 18e5, xc = globalThis.crypto?.randomUUID?.() || String(Date.now()), Sc = null;
function Cc() {
	for (let [e, t] of vc) Date.now() - t.updated > bc && vc.delete(e);
	for (; vc.size > yc;) vc.delete(vc.keys().next().value);
}
function wc(e) {
	e.draft && (e.draft.updated = Date.now(), vc.delete(e.scope), vc.set(e.scope, e.draft), Cc());
}
function Tc(e = {}) {
	Vc();
	let t = e.root || document.querySelector("#live-session[data-runtime=\"true\"]"), n = t?.querySelector("[data-react-attention]"), r = t?.querySelector("#live-composer-seat");
	if (!t || !n || !r) return;
	let i = [
		e.project ?? t.dataset.project,
		e.session ?? t.dataset.session,
		e.instance ?? t.dataset.instance
	];
	for (let [e, t] of vc) t.identity[0] === i[0] && t.identity[1] === i[1] && t.identity[2] !== i[2] && vc.delete(e);
	Cc();
	let a = {
		api: e,
		root: t,
		region: n,
		seat: r,
		react: (0, d.createRoot)(n),
		controller: new AbortController(),
		identity: i,
		scope: JSON.stringify([e.nonce ?? xc, ...i]),
		key: "",
		kind: "input",
		pending: null,
		turn: "",
		draft: null,
		state: {},
		feedback: "",
		composing: !1,
		form: (0, l.createRef)(),
		body: (0, l.createRef)(),
		layoutFrame: 0,
		layoutMetrics: null
	};
	Sc = a;
	let o = { signal: a.controller.signal }, s = () => zc(a);
	if (window.addEventListener("resize", s, o), window.visualViewport?.addEventListener("resize", s, o), window.visualViewport?.addEventListener("scroll", s, o), globalThis.ResizeObserver) {
		a.observer = new ResizeObserver(s), a.observer.observe(t);
		for (let e of t.children) e !== r && e.id !== "live-stream" && e.tagName !== "DIALOG" && a.observer.observe(e);
	}
	kc(a), Ec(a, !1);
}
function Ec(e, t) {
	t || (e.layoutMetrics = null, e.root.classList.remove("attention-compact", "attention-constrained")), e.api.onTakeover?.(t);
}
function Dc(e, t = {}) {
	if (!Sc) return { takingOver: !1 };
	let n = Sc;
	if (n.state = {
		...t,
		safe: t.safe === !0 && !t.busy
	}, e) {
		if (![
			e.project_id,
			e.session_id,
			e.instance_id
		].every((e, t) => e === void 0 || e === n.identity[t])) vc.delete(n.scope), n.state.safe = !1;
		else {
			typeof e.cancel_token == "string" && (n.turn = e.cancel_token);
			let t = e.permission || e.input;
			if (!t) vc.delete(n.scope), n.pending = null, n.draft = null, n.key = "", n.feedback = "", n.composing = !1;
			else {
				let r = e.permission ? "permission" : "input", i = pc(r, t, n.turn);
				if (i !== n.key) {
					let e = vc.get(n.scope);
					n.draft = e?.key === i ? e : {
						key: i,
						identity: n.identity,
						page: 0,
						collapsed: !1,
						answers: Object.create(null),
						updated: Date.now()
					}, n.key = i, n.kind = r, n.feedback = "", n.composing = !1, wc(n);
				}
				n.pending = t;
			}
		}
	}
	let r = !!n.pending;
	return kc(n), Ec(n, r), r ? zc(n) : n.api.onLayout?.(), { takingOver: r };
}
function Oc(e, t) {
	return Sc === e && e.key === t && e.region.isConnected;
}
function kc(e) {
	if (Sc !== e) return;
	let t = e.key, n = () => Oc(e, t) && e.state.safe === !0;
	(0, u.flushSync)(() => e.react.render(e.pending && e.draft ? /* @__PURE__ */ (0, D.jsx)(_c, {
		kind: e.kind,
		pending: e.pending,
		draft: e.draft,
		state: e.state,
		feedback: e.feedback,
		formRef: e.form,
		bodyRef: e.body,
		onGuard: (n) => jc(e, t, n),
		onSubmit: (n) => Mc(e, t, n),
		onPage: (t) => {
			n() && e.draft && lc(e.pending) && (e.draft.page = Math.max(0, Math.min(e.pending.questions.length - 1, e.draft.page + t)), e.feedback = "", Pc(e, !0));
		},
		onCollapse: () => {
			n() && e.draft && (e.draft.collapsed = !e.draft.collapsed, Pc(e, !1));
		},
		onContinue: () => {
			n() && Lc(e);
		},
		onAnswer: (t, r, i, a) => {
			n() && Fc(e, t, r, i, a);
		},
		onKeydown: (n) => {
			Oc(e, t) && Rc(e, n);
		},
		onComposition: (n) => {
			Oc(e, t) && (e.composing = n);
		}
	}, t) : null));
}
function Ac(e) {
	e.preventDefault(), e.stopPropagation(), e.nativeEvent.stopImmediatePropagation();
}
function jc(e, t, n) {
	let r = n.target instanceof Element ? n.target.closest("button") : null;
	if (!r) return;
	let i = r.hasAttribute("data-runtime-abort");
	(!Oc(e, t) || r.disabled || (i ? !e.state.canStop : !e.state.safe) || r.dataset.permission === "allow" && fc(e.pending) || r.hasAttribute("data-permission") && (!oc(e.pending) || !cc(e.pending.id) || r.dataset.requestId !== e.pending.id)) && Ac(n);
}
function Mc(e, t, n) {
	(!Oc(e, t) || n.currentTarget !== e.form.current || !Ic(e, !0)) && Ac(n);
}
function Nc(e, t = !1) {
	let n = e.form.current?.querySelector("fieldset:not([hidden])");
	(t ? n?.querySelector("textarea") : n?.querySelector("input:checked") || n?.querySelector("input, textarea"))?.focus({ preventScroll: !0 });
}
function Pc(e, t) {
	wc(e), kc(e), e.body.current && (e.body.current.scrollTop = 0), t && !e.draft?.collapsed && Nc(e), zc(e);
}
function Fc(e, t, n, r = !1, i = !1) {
	e.draft && lc(e.pending) && e.pending.questions.some((e) => e.id === t) && (e.draft.answers[t] = {
		...e.draft.answers[t] || {
			selected: null,
			custom: ""
		},
		...n
	}, e.feedback = "", wc(e), r && e.draft.page < e.pending.questions.length - 1 ? (e.draft.page++, Pc(e, !0)) : (kc(e), i && Nc(e, !0), zc(e)));
}
function Ic(e, t = !1) {
	if (!e.state.safe || !e.draft || !lc(e.pending)) return !1;
	let n = dc(e.pending.questions, e.draft, t);
	return n < 0 ? (e.feedback = "", kc(e), !0) : (e.draft.page = n, e.draft.collapsed = !1, e.feedback = t ? "Answer every question before submitting." : "Choose an option or enter an answer to continue.", Pc(e, !0), !1);
}
function Lc(e) {
	if (Ic(e) && e.draft && lc(e.pending)) {
		if (e.draft.page < e.pending.questions.length - 1) e.draft.page++, Pc(e, !0);
		else {
			let t = e.form.current?.querySelector("[data-attention-continue]");
			t && e.form.current?.requestSubmit(t);
		}
	}
}
function Rc(e, t) {
	t.key !== "Enter" || t.shiftKey || t.nativeEvent.isComposing || t.keyCode === 229 || e.composing || (t.preventDefault(), e.state.safe && Lc(e));
}
function zc(e) {
	Sc === e && e.pending && !e.layoutFrame && (e.layoutFrame = requestAnimationFrame(() => {
		e.layoutFrame = 0, Sc === e && e.pending && !e.region.hidden && Bc(e);
	}));
}
function Bc(e) {
	let { root: t, seat: n } = e, r = t.getBoundingClientRect(), i = window.visualViewport ? window.visualViewport.offsetTop + window.visualViewport.height : window.innerHeight, a = Math.min(r.bottom, i) - r.top;
	for (let e of t.children) {
		if (e === n || e.id === "live-stream") continue;
		let t = getComputedStyle(e);
		t.display !== "none" && t.position !== "absolute" && t.position !== "fixed" && (a -= e.getBoundingClientRect().height + (parseFloat(t.marginTop) || 0) + (parseFloat(t.marginBottom) || 0));
	}
	let o = a < 180 || (window.visualViewport?.height || window.innerHeight) <= 420, s = t.classList.contains("attention-compact") !== o;
	s && t.classList.toggle("attention-compact", o);
	let c = t.querySelector("#live-stream");
	if (c) {
		let e = getComputedStyle(c);
		for (let t of [
			"paddingTop",
			"paddingBottom",
			"borderTopWidth",
			"borderBottomWidth"
		]) a -= parseFloat(e[t]) || 0;
	}
	let l = a < 96, u = t.classList.contains("attention-constrained") !== l;
	u && t.classList.toggle("attention-constrained", l);
	let d = Math.max(96, Math.floor(a)), f = parseFloat(n.style.getPropertyValue("--attention-seat-height"));
	(!Number.isFinite(f) || Math.abs(f - d) > .5) && n.style.setProperty("--attention-seat-height", d + "px");
	let p = n.getBoundingClientRect(), m = [
		p.top,
		p.width,
		p.height,
		r.height
	];
	(!e.layoutMetrics || m.some((t, n) => Math.abs(t - e.layoutMetrics[n]) > .5) || s || u) && (e.layoutMetrics = m, e.api.onLayout?.());
}
function Vc({ clearDrafts: e = !1 } = {}) {
	if (Sc) {
		let e = Sc;
		wc(e), e.controller.abort(), e.observer?.disconnect(), e.layoutFrame && cancelAnimationFrame(e.layoutFrame), e.seat.style.removeProperty("--attention-seat-height"), Ec(e, !1), Sc = null, (0, u.flushSync)(() => e.react.unmount());
	}
	e ? vc.clear() : Cc();
}
//#endregion
//#region src/attention/index.ts
var Hc = {
	init: Tc,
	render: Dc,
	dispose: Vc
};
//#endregion
//#region src/reader/Controls.tsx
function Uc({ hidden: e, follow: t }) {
	return /* @__PURE__ */ (0, D.jsx)("button", {
		className: "button jump-latest",
		type: "button",
		id: "jump-latest",
		hidden: e,
		"aria-label": "Jump to latest",
		title: "Jump to latest",
		onClick: (e) => {
			e.stopPropagation(), t();
		},
		children: /* @__PURE__ */ (0, D.jsx)("svg", {
			viewBox: "0 0 24 24",
			"aria-hidden": "true",
			fill: "none",
			stroke: "currentColor",
			strokeWidth: "1.7",
			strokeLinecap: "round",
			strokeLinejoin: "round",
			children: /* @__PURE__ */ (0, D.jsx)("path", { d: "M12 5v14m-6-6 6 6 6-6" })
		})
	});
}
function Wc(e, t) {
	let n = (0, d.createRoot)(e), r;
	return {
		render(e) {
			r !== e && (r = e, (0, u.flushSync)(() => n.render(/* @__PURE__ */ (0, D.jsx)(Uc, {
				hidden: e,
				follow: t
			}))));
		},
		dispose() {
			(0, u.flushSync)(() => n.unmount());
		}
	};
}
//#endregion
//#region src/reader/controller.ts
var Gc = /* @__PURE__ */ new Map(), Kc = 40, qc = 24, Jc = .5, Yc = (e, t) => t.querySelector(e), Xc = (e, t) => Math.max(0, Math.min(e, t));
function Zc(e, t) {
	let n = Yc("#live-stream", e), r = Yc("#live-transcript", e);
	if (!n || !r) return null;
	let i = n, a = r, o = e.closest(".conversation-pane") || e, s = Yc("#live-composer-seat", o), c = Yc("[data-react-reader-controls]", e);
	if (!c) return null;
	let l = Wc(c, de), u = [], d = Gc.get(t), f = d?.following !== !1, p = d?.anchor || null, m = i.scrollTop, h = !1, g = !1, _ = 0, v, y, b, x, S = () => Math.max(0, i.scrollHeight - i.clientHeight), ee = () => Xc(i.scrollTop, S());
	function C(e, t, n, r) {
		e.addEventListener(t, n, r), u.push(() => e.removeEventListener(t, n, r));
	}
	function w(e) {
		let t = Xc(e, S());
		Math.abs(i.scrollTop - t) > Jc && (i.scrollTop = t), m = ee();
	}
	function te() {
		let e = [...i.querySelectorAll("[data-message-id],[data-activity-id]")], t = /* @__PURE__ */ new Map(), n = e.map((e) => ({
			node: e,
			key: e.dataset.messageId ? `message:${e.dataset.messageId}` : e.dataset.activityId ? `activity:${e.dataset.activityId}` : ""
		}));
		for (let e of n) t.set(e.key, (t.get(e.key) || 0) + 1);
		return n.filter((e) => e.key && t.get(e.key) === 1 && !e.node.hidden);
	}
	function T() {
		let e = i.getBoundingClientRect(), t = [];
		for (let n of te()) {
			let r = n.node.getBoundingClientRect();
			if (!(r.bottom <= e.top || r.height === 0) && (t.push({
				key: n.key,
				offset: r.top
			}), t.length === 8)) break;
		}
		return {
			top: ee(),
			candidates: t
		};
	}
	function ne() {
		if (t) for (Gc.delete(t), Gc.set(t, {
			following: f,
			anchor: f ? null : p
		}); Gc.size > Kc;) Gc.delete(Gc.keys().next().value);
	}
	function E() {
		e.dataset.scrollFollowing = String(f), l.render(f || S() <= Jc);
	}
	function re() {
		if (!p) {
			p = T();
			return;
		}
		let e = new Map(te().map((e) => [e.key, e.node])), t = p.candidates.find((t) => e.has(t.key));
		if (t) {
			let n = e.get(t.key).getBoundingClientRect().top;
			w(ee() + n - t.offset);
		} else w(p.top);
		p = T();
	}
	function ie() {
		let e = ee(), t = e - Xc(m, S());
		return Math.abs(t) <= Jc ? (m = e, !1) : (f = t > 0 && S() - e <= qc, m = e, p = f ? null : T(), E(), ne(), !0);
	}
	function D() {
		g || (ie(), E());
	}
	function ae() {
		let e = s?.querySelector("#live-prompt");
		if (!e || !e.getClientRects().length) return;
		let t = getComputedStyle(e), n = e.clientWidth, r = t.minHeight, i = t.maxHeight;
		if (x?.node === e && x.value === e.value && x.width === n && x.minimum === r && x.maximum === i) return;
		let a = e.scrollTop, o = parseFloat(t.borderTopWidth) + parseFloat(t.borderBottomWidth), c = parseFloat(t.paddingTop) + parseFloat(t.paddingBottom);
		e.style.height = "0px";
		let l = e.scrollHeight + (t.boxSizing === "border-box" ? o : -c);
		e.style.height = `${Math.max(parseFloat(r) || 0, Math.min(l, parseFloat(i) || Infinity))}px`, e.scrollTop = a, x = {
			node: e,
			value: e.value,
			width: e.clientWidth,
			minimum: r,
			maximum: i
		};
	}
	function oe() {
		s && s.style.setProperty("--scroll-seat-limit", `${Math.max(64, o.getBoundingClientRect().height * .55)}px`), ae();
		let t = e.getBoundingClientRect(), n = i.getBoundingClientRect(), r = a.getBoundingClientRect(), c = s && !s.hidden ? s.getBoundingClientRect().top : t.bottom, l = Math.max(0, t.bottom - Math.min(n.bottom, c)) + 16, u = Math.max(16, t.right - Math.min(r.right, n.right - 16));
		return e.style.setProperty("--scroll-jump-bottom", `${l}px`), e.style.setProperty("--scroll-jump-right", `${u}px`), [
			i.scrollHeight,
			i.clientHeight,
			i.clientWidth,
			t.height,
			c,
			r.width
		].join(":");
	}
	function se(e = !1) {
		if (g || h) return;
		ie();
		let t = oe();
		(e || t !== b) && (f ? w(S()) : re()), b = t, E(), ne();
	}
	function ce() {
		g || _ || (_ = requestAnimationFrame(() => {
			_ = 0, se(!0);
		}));
	}
	function le() {
		g || h || (ie(), !f && !p && (p = T()), h = !0);
	}
	function ue() {
		g || (h && (m = ee()), h = !1, se(!0), fe());
	}
	function de() {
		g || (f = !0, p = null, b = oe(), w(S()), E(), ne());
	}
	function fe() {
		if (v) {
			v.disconnect(), v.observe(o), v.observe(e), v.observe(i), s && v.observe(s);
			for (let e of i.children) v.observe(e);
		}
	}
	function pe() {
		let e = window.visualViewport;
		e && Math.abs(e.scale - 1) < .01 && e.height > 0 ? document.documentElement.style.setProperty("--snow-visual-height", `${e.height}px`) : document.documentElement.style.removeProperty("--snow-visual-height"), ce();
	}
	C(i, "scroll", D, { passive: !0 }), s && C(s, "input", (e) => {
		e.target instanceof Element && e.target.id === "live-prompt" && se(!0);
	});
	function me(e) {
		for (let t = e instanceof Element ? e : null; t && t !== i; t = t.parentElement) if (t.scrollHeight > t.clientHeight + 1 && /auto|scroll/.test(getComputedStyle(t).overflowY)) return !0;
		return !1;
	}
	function he() {
		g || f && S() > Jc && (f = !1, p = T(), E(), ne());
	}
	function ge(e) {
		!e.defaultPrevented && e.deltaY < 0 && !e.ctrlKey && !me(e.target) && he();
	}
	C(i, "wheel", ge, { passive: !0 });
	function _e(e) {
		if (g || e.ctrlKey || e.defaultPrevented || !e.deltaY || !(e.target instanceof Element) || !i.contains(e.target) || !e.target.closest?.("[data-chat-width-handle]")) return;
		ge(e), e.preventDefault();
		let t = e.deltaMode === 2 ? i.clientHeight : e.deltaMode === 1 ? parseFloat(getComputedStyle(i).lineHeight) || 16 : 1;
		i.scrollTop += e.deltaY * t;
	}
	let ve;
	return C(i, "touchstart", (e) => {
		ve = e.touches.length === 1 ? e.touches[0].clientY : void 0;
	}, { passive: !0 }), C(i, "touchmove", (e) => {
		if (ve === void 0 || e.touches.length !== 1) return;
		let t = e.touches[0].clientY;
		t > ve && !me(e.target) && he(), ve = t;
	}, { passive: !0 }), C(i, "keydown", (e) => {
		e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey || e.target instanceof Element && e.target.closest("input,textarea,select,[contenteditable=\"true\"]") || ([
			"ArrowUp",
			"PageUp",
			"Home"
		].includes(e.key) || e.key === " " && e.shiftKey) && (me(e.target) || he());
	}), C(window, "resize", pe, { passive: !0 }), window.visualViewport && (C(window.visualViewport, "resize", pe, { passive: !0 }), C(window.visualViewport, "scroll", ce, { passive: !0 })), typeof ResizeObserver == "function" && (v = new ResizeObserver(ce), fe()), typeof MutationObserver == "function" && (y = new MutationObserver(() => {
		fe(), ce();
	}), y.observe(i, {
		childList: !0,
		subtree: !0,
		characterData: !0,
		attributes: !0,
		attributeFilter: ["open", "hidden"]
	})), b = oe(), d && !f ? (w(d.anchor?.top || 0), re()) : w(S()), E(), ne(), pe(), {
		beforeUpdate: le,
		afterUpdate: ue,
		follow: de,
		wheelFromHandle: _e,
		onLayout: ce,
		userIntent: he,
		dispose() {
			if (!g) {
				ie(), ne(), g = !0, _ && cancelAnimationFrame(_), v?.disconnect(), y?.disconnect();
				for (let e of u) e();
				l.dispose(), p = null, x = void 0, e.removeAttribute("data-scroll-following");
				for (let t of ["--scroll-jump-bottom", "--scroll-jump-right"]) e.style.removeProperty(t);
				s?.style.removeProperty("--scroll-seat-limit"), document.documentElement.style.removeProperty("--snow-visual-height");
			}
		}
	};
}
//#endregion
//#region src/reader/index.ts
var Qc = null;
function $c() {
	Qc?.dispose(), Qc = null;
}
function el(e, t = "") {
	return $c(), e && (Qc = Zc(e, String(t || ""))), Qc;
}
var tl = Object.freeze({
	init: el,
	dispose: $c,
	beforeUpdate: () => Qc?.beforeUpdate(),
	afterUpdate: () => Qc?.afterUpdate(),
	follow: () => Qc?.follow(),
	wheelFromHandle: (e) => Qc?.wheelFromHandle(e),
	onLayout: () => Qc?.onLayout(),
	userIntent: () => Qc?.userIntent()
}), nl = ["left", "right"], rl = "Drag to resize conversation. Arrow keys adjust; Shift adjusts faster. Home restores automatic width. Escape cancels a drag.";
function il({ refs: e, views: t, dragging: n }) {
	return /* @__PURE__ */ (0, D.jsx)(D.Fragment, { children: nl.map((r, i) => {
		let a = t[i];
		return /* @__PURE__ */ (0, D.jsx)("div", {
			ref: e[i],
			className: "chat-width-handle",
			"data-chat-width-handle": r,
			role: "separator",
			"aria-orientation": "vertical",
			"aria-label": `Conversation width, ${r} edge`,
			"aria-controls": "live-transcript live-composer-seat",
			title: rl,
			hidden: a.hidden,
			tabIndex: a.hidden ? -1 : 0,
			"aria-valuemin": a.minimum,
			"aria-valuemax": a.maximum,
			"aria-valuenow": a.value,
			"aria-valuetext": a.text,
			"data-dragging": n === r ? "true" : void 0
		}, r);
	}) });
}
function al(e) {
	let t = (0, d.createRoot)(e), n = nl.map(() => (0, l.createRef)()), r = nl.map(() => ({
		hidden: !0,
		minimum: 640,
		maximum: 640,
		value: 640,
		text: "640 pixels; automatic width"
	})), i = null, a = () => (0, u.flushSync)(() => t.render(/* @__PURE__ */ (0, D.jsx)(il, {
		refs: n,
		views: r,
		dragging: i
	})));
	return a(), {
		handles: n.map((e) => e.current),
		render(e) {
			e.every((e, t) => {
				let n = r[t];
				return e.hidden === n.hidden && e.minimum === n.minimum && e.maximum === n.maximum && e.value === n.value && e.text === n.text;
			}) || (r = e, a());
		},
		dragging(e) {
			e !== i && (i = e, a());
		},
		dispose() {
			(0, u.flushSync)(() => t.unmount());
		}
	};
}
//#endregion
//#region src/width/controller.ts
var ol = () => window.SnowScroll, sl = "snow-manager-chat-width", cl = 640, ll = 176;
function ul() {
	try {
		let e = localStorage.getItem(sl);
		if (e === null || e.length > 64 || !e.trim()) return null;
		let t = Number(e);
		return Number.isFinite(t) && t > 0 ? t : null;
	} catch {
		return null;
	}
}
function dl(e) {
	let t = e.closest(".conversation-pane") || e, n = e.querySelector("#live-stream"), r = e.querySelector("#live-transcript"), i = e.querySelector("[data-react-width-controls]");
	if (!n || !r || !i) return null;
	let a = n, o = r, s = new AbortController(), c = { signal: s.signal }, l = al(i), u = l.handles, d = ul(), f = null, p = 0, m = !1, h, g = () => t.getBoundingClientRect().width, _ = () => Math.max(cl, g() - ll), v = (e) => Math.max(cl, Math.min(e, _())), y = () => d === null ? Math.max(680, Math.min(g() * .64, 920)) : v(d);
	function b(e) {
		d = e;
		try {
			e === null ? localStorage.removeItem(sl) : localStorage.setItem(sl, String(e));
		} catch {}
	}
	function x() {
		let n = a.getBoundingClientRect(), r = o.getBoundingClientRect(), i = t.getBoundingClientRect(), s = e.querySelector(".live-header")?.getBoundingClientRect(), c = e.querySelector("#live-composer-seat")?.getBoundingClientRect(), p = Math.max(0, n.top, s?.bottom || 0), m = Math.min(innerHeight, n.bottom, c?.top ?? innerHeight), h = [];
		for (let e of u) {
			let t = e.dataset.chatWidthHandle === "left", n = t ? r.left - i.left - 48 : i.right - r.right - 48, o = Math.max(0, Math.min(40, n)), s = o < 1 || m <= p || !a.getClientRects().length;
			e.style.left = `${t ? r.left - 24 - o : r.right + 24}px`, e.style.top = `${p}px`, e.style.width = `${o}px`, e.style.height = `${Math.max(0, m - p)}px`;
			let c = Math.round(f ? f.width : y());
			h.push({
				hidden: s,
				minimum: cl,
				maximum: Math.round(Math.max(_(), y())),
				value: c,
				text: `${c} pixels; ${d === null && !f ? "automatic" : "custom"} width`
			});
		}
		l.render(h);
	}
	function S() {
		if (m || !e.isConnected) return;
		let n = g();
		if (!Number.isFinite(n) || n <= 0) return;
		let r = `${n}px`, i = f ? `${v(f.width)}px` : d === null ? "" : `${y()}px`;
		(t.style.getPropertyValue("--conversation-column-width") !== r || t.style.getPropertyValue("--chat-content-width") !== i) && (ol()?.beforeUpdate(), t.style.setProperty("--conversation-column-width", r), i ? t.style.setProperty("--chat-content-width", i) : t.style.removeProperty("--chat-content-width"), ol()?.afterUpdate()), x();
	}
	function ee() {
		m || p || (p = requestAnimationFrame(() => {
			p = 0, S();
		}));
	}
	function C(e = !1, t) {
		if (!f) return;
		let n = f;
		e && t !== void 0 && Number.isFinite(t) && t !== n.origin && b(v(n.base + (t - n.origin) * n.direction * 2)), f = null, l.dragging(null), document.documentElement.classList.remove("chat-width-dragging"), n.handle.hasPointerCapture(n.pointer) && n.handle.releasePointerCapture(n.pointer), S();
	}
	function w() {
		m || (C(), b(null), S());
	}
	for (let e of u) {
		let t = e.dataset.chatWidthHandle;
		e.addEventListener("wheel", (e) => ol()?.wheelFromHandle(e), {
			...c,
			passive: !1
		}), e.addEventListener("pointerdown", (n) => {
			n.button !== 0 || n.isPrimary === !1 || f || e.hidden || (n.preventDefault(), e.setPointerCapture(n.pointerId), e.focus({ preventScroll: !0 }), f = {
				handle: e,
				pointer: n.pointerId,
				base: y(),
				origin: n.clientX,
				direction: t === "left" ? -1 : 1,
				width: y()
			}, l.dragging(t), document.documentElement.classList.add("chat-width-dragging"));
		}, c), e.addEventListener("pointermove", (t) => {
			e.style.setProperty("--width-pointer-y", `${t.clientY - e.getBoundingClientRect().top}px`), f && f.handle === e && f.pointer === t.pointerId && (f.width = v(f.base + (t.clientX - f.origin) * f.direction * 2), ee());
		}, c), e.addEventListener("pointerup", (t) => {
			f?.handle === e && f.pointer === t.pointerId && C(!0, t.clientX);
		}, c);
		for (let t of ["pointercancel", "lostpointercapture"]) e.addEventListener(t, (t) => {
			f?.handle === e && f.pointer === t.pointerId && C();
		}, c);
		e.addEventListener("keydown", (e) => {
			if (!(e.altKey || e.ctrlKey || e.metaKey)) {
				if (e.key === "Home") e.preventDefault(), w();
				else if (["ArrowLeft", "ArrowRight"].includes(e.key)) {
					e.preventDefault(), C();
					let n = (e.key === "ArrowRight" ? 1 : -1) * (t === "left" ? -1 : 1), r = y(), i = v(r + n * (e.shiftKey ? 40 : 10));
					if (n > 0 && i < r) return;
					b(i), S();
				}
			}
		}, c);
	}
	return document.addEventListener("keydown", (e) => {
		e.key === "Escape" && f && (e.preventDefault(), e.stopPropagation(), C());
	}, {
		...c,
		capture: !0
	}), window.addEventListener("blur", () => C(), c), document.addEventListener("visibilitychange", () => {
		document.hidden && C();
	}, c), window.addEventListener("resize", () => {
		C(), ee();
	}, c), document.addEventListener("scroll", ee, {
		...c,
		capture: !0,
		passive: !0
	}), window.visualViewport && (window.visualViewport.addEventListener("resize", () => {
		C(), ee();
	}, c), window.visualViewport.addEventListener("scroll", ee, c)), typeof ResizeObserver == "function" && (h = new ResizeObserver(() => {
		f && t.style.getPropertyValue("--conversation-column-width") !== `${g()}px` && C(), ee();
	}), h.observe(t), h.observe(a), h.observe(o)), S(), {
		reset: w,
		dispose() {
			m || (m = !0, C(), s.abort(), h?.disconnect(), p && cancelAnimationFrame(p), l.dispose(), t.style.removeProperty("--conversation-column-width"), t.style.removeProperty("--chat-content-width"));
		}
	};
}
//#endregion
//#region src/width/index.ts
var fl = null;
function pl() {
	fl?.dispose(), fl = null;
}
function ml(e) {
	return pl(), e && (fl = dl(e)), fl;
}
var hl = Object.freeze({
	init: ml,
	dispose: pl,
	reset: () => fl?.reset()
}), gl = {
	rows: 100,
	projects: 100,
	pages: 25,
	reads: 100,
	response: 262144
}, _l = /^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/;
function vl(e) {
	if (!e || typeof e != "object" || Array.isArray(e)) throw Error("Invalid shell data");
	return e;
}
function yl(e, t) {
	if (typeof e != "string" || e.length > t) throw Error("Invalid shell text");
	return e;
}
function H(e) {
	if (typeof e != "boolean") throw Error("Invalid shell flag");
	return e;
}
function bl(e) {
	if (!Array.isArray(e) || e.length > gl.rows) throw Error("Invalid session rows");
	let t = /* @__PURE__ */ new Set();
	return e.map((e) => {
		let n = vl(e), r = yl(n.session_id, 256);
		if (!r || t.has(r)) throw Error("Invalid session identity");
		return t.add(r), {
			session_id: r,
			name: yl(n.name, 4096)
		};
	});
}
function xl(e) {
	if (e === null) return null;
	let t = vl(e), n = yl(t.project, 36), r = yl(t.session, 256), i = yl(t.instance, 256);
	if (!_l.test(n) || !r || !i) throw Error("Invalid live identity");
	return {
		project: n,
		session: r,
		instance: i,
		title: yl(t.title, 4096),
		renameAvailable: H(t.renameAvailable),
		renameDisabled: H(t.renameDisabled),
		newDisabled: H(t.newDisabled)
	};
}
function Sl(e) {
	let t = vl(e);
	if (!Array.isArray(t.projects) || t.projects.length > gl.projects) throw Error("Invalid shell projects");
	let n = /* @__PURE__ */ new Set(), r = t.projects.map((e) => {
		let t = vl(e), r = yl(t.id, 36), i = yl(t.name, 128);
		if (!_l.test(r) || n.has(r) || new TextEncoder().encode(i).length > 128) throw Error("Invalid project identity");
		return n.add(r), {
			id: r,
			name: i,
			path: yl(t.path, 4096),
			available: H(t.available),
			trustRemembered: H(t.trustRemembered),
			skillsEnabled: H(t.skillsEnabled),
			pinned: H(t.pinned)
		};
	}), i = yl(t.project, 36), a = xl(t.live), o = yl(t.networkProfile, 32);
	if (i && !n.has(i) || a && (a.project !== i || !n.has(a.project))) throw Error("Invalid selected project");
	if (o !== "local" && o !== "trusted-lan-http") throw Error("Invalid network profile");
	return {
		csrf: yl(t.csrf, 512),
		version: yl(t.version, 128),
		view: yl(t.view, 32),
		project: i,
		session: yl(t.session, 256),
		projects: r,
		sessions: bl(t.sessions),
		live: a,
		hostSettingsEnabled: H(t.hostSettingsEnabled),
		networkProfile: o,
		pairingCode: yl(t.pairingCode, 128)
	};
}
function Cl(e, t, n, r, i) {
	let a = vl(e), o = yl(a.instance_id, 256);
	if (a.project_id !== t || n > 0 && o !== r || i?.project === t && o !== i.instance) throw Error("Stale inventory");
	let s = Number.isSafeInteger(a.next_offset) && a.next_offset > n ? a.next_offset : 0;
	return {
		project: t,
		instance: o,
		rows: bl(a.sessions),
		available: a.available === !0,
		deleteSupported: a.delete_supported === !0 && typeof a.active_session_id == "string",
		activeSession: typeof a.active_session_id == "string" ? yl(a.active_session_id, 256) : "",
		nextOffset: s,
		hasMore: a.has_more === !0 && s > 0,
		truncated: a.truncated === !0
	};
}
async function wl(e, t) {
	if (!e.body) throw Error("Missing response");
	let n = e.body.getReader(), r = new TextDecoder(), i = 0, a = "";
	try {
		for (;;) {
			let e = await n.read();
			if (e.done) break;
			if (i += e.value.length, i > t) throw Error("Response too large");
			a += r.decode(e.value, { stream: !0 });
		}
		return a + r.decode();
	} finally {
		await n.cancel(), n.releaseLock();
	}
}
var Tl = (e, t = {}) => "/?" + new URLSearchParams({
	view: "projects",
	project: e,
	...t
});
function El(e) {
	return !!e && e.supported && e.available && !e.active;
}
//#endregion
//#region src/shell/controller.ts
var Dl = (e, t) => ({
	project: e,
	expanded: t,
	rows: [],
	instance: "",
	loaded: !1,
	available: !0,
	deleteSupported: !1,
	activeSession: "",
	loading: !1,
	error: "",
	nextOffset: 0,
	hasMore: !1,
	truncated: !1,
	pages: 0
}), U = new class {
	snapshot = {
		bootstrap: null,
		groups: /* @__PURE__ */ new Map(),
		live: null,
		query: "",
		searchOpen: !1,
		hideSessions: !1,
		pinnedOnly: !1,
		collapsed: !1,
		navOpen: !1,
		narrow: !1,
		theme: "dark",
		settings: null,
		settingsReturn: null,
		menu: null,
		deletion: null
	};
	listeners = /* @__PURE__ */ new Set();
	requests = /* @__PURE__ */ new Map();
	sidebarMemory = null;
	rememberSidebar() {
		let e = document.getElementById("project-navigation");
		if (!e) return;
		let t = document.activeElement instanceof HTMLElement && e.contains(document.activeElement) ? document.activeElement : null, n = e.querySelector("[data-sidebar-search]");
		this.sidebarMemory = {
			list: e.querySelector(".project-tree")?.scrollTop || 0,
			rail: e.scrollTop,
			focused: t ? {
				project: t.closest("[data-sidebar-project]")?.dataset.sidebarProject || "",
				session: t.closest("[data-shell-session]")?.dataset.shellSession || "",
				selector: [
					"[data-sidebar-search]",
					"[data-workspace-toggle]",
					"[data-shell-project-new]",
					"[data-shell-project-menu]",
					"[data-shell-session-menu]",
					"[data-settings-open]",
					"[data-sidebar-collapse]"
				].find((e) => t.matches(e)) || "",
				href: t.getAttribute("href") || ""
			} : null,
			selection: t === n && n ? [n.selectionStart || 0, n.selectionEnd || 0] : null
		};
	}
	restoreSidebar() {
		let e = this.sidebarMemory, t = document.getElementById("project-navigation");
		if (!e || !t) return;
		this.sidebarMemory = null;
		let n = t.querySelector(".project-tree");
		n && (n.scrollTop = e.list), t.scrollTop = e.rail;
		let r = e.focused, i = document.activeElement;
		if (!r || i && i !== document.body && i.id !== "workspace" && i.isConnected) return;
		let a = [...t.querySelectorAll("[data-sidebar-project]")].find((e) => e.dataset.sidebarProject === r.project), o = a && [...a.querySelectorAll("[data-shell-session]")].find((e) => e.dataset.shellSession === r.session) || a || t, s = r.selector ? o.querySelector(r.selector) : [...o.querySelectorAll("a")].find((e) => e.getAttribute("href") === r.href);
		if (s?.focus({ preventScroll: !0 }), e.selection && s instanceof HTMLInputElement && s.setSelectionRange(...e.selection), s && n?.contains(s)) {
			let e = n.getBoundingClientRect(), t = s.getBoundingClientRect();
			t.top < e.top ? n.scrollTop -= e.top - t.top : t.bottom > e.bottom && (n.scrollTop += t.bottom - e.bottom);
		}
	}
	owner = {};
	mounted = !1;
	reads = 0;
	observer;
	navigate = (e) => {
		window.location.assign(e);
	};
	command = (e) => document.dispatchEvent(new CustomEvent("snow:shell-command", { detail: e }));
	getSnapshot = () => this.snapshot;
	subscribe = (e) => (this.listeners.add(e), () => {
		this.listeners.delete(e);
	});
	publish(e) {
		this.snapshot = {
			...this.snapshot,
			...e
		}, this.listeners.forEach((e) => e());
	}
	group(e, t) {
		let n = this.snapshot.groups.get(e);
		if (!n) return;
		let r = new Map(this.snapshot.groups);
		r.set(e, {
			...n,
			...t
		}), this.publish({ groups: r });
	}
	mount(e, t, n) {
		this.owner = {}, this.mounted = !0, this.reads = 0, t && (this.navigate = t), n && (this.command = n);
		let r = /* @__PURE__ */ new Map();
		for (let t of e.projects) {
			let n = this.snapshot.groups.get(t.id);
			r.set(t.id, n ? {
				...n,
				loading: !1,
				deleteSupported: !1,
				loaded: !1,
				pages: 0
			} : Dl(t.id, t.id === e.project));
		}
		let i = r.get(e.project);
		if (i) {
			let t = new Map(i.rows.map((e) => [e.session_id, e]));
			e.sessions.forEach((e) => t.set(e.session_id, e)), i.rows = [...t.values()].slice(0, gl.rows);
		}
		this.publish({
			bootstrap: e,
			groups: r,
			live: null,
			deletion: null,
			menu: null,
			settings: null,
			theme: document.documentElement.dataset.theme === "light" ? "light" : "dark"
		}), this.updateLive(e.live), this.initialize();
		for (let e of this.snapshot.groups.values()) e.expanded && !this.snapshot.hideSessions && this.load(e.project);
	}
	dispose() {
		this.rememberSidebar(), this.mounted = !1, this.owner = {}, this.observer?.disconnect(), this.preemptInventory(), this.publish({
			menu: null,
			deletion: null,
			settings: null
		});
	}
	initialize = () => {
		this.observer?.disconnect();
		let e = document.querySelector("#live-session[data-runtime=\"true\"]");
		e && (this.observer = new MutationObserver(() => this.syncCurrent(e)), this.observer.observe(e, {
			attributes: !0,
			subtree: !0,
			childList: !0,
			characterData: !0,
			attributeFilter: [
				"disabled",
				"data-session",
				"data-instance"
			]
		}), document.querySelectorAll("[data-workflow-new], [data-workflow-switch-confirm], [data-workflow-rename], [data-workflow-rename-form] button[type=submit]").forEach((e) => this.observer?.observe(e, {
			attributes: !0,
			attributeFilter: ["disabled"]
		})), this.syncCurrent(e));
	};
	syncCurrent = (e) => {
		if (!e?.isConnected || e.dataset.runtime !== "true") return;
		let t = document.querySelector("[data-workflow-rename], [data-workflow-rename-form] button[type=\"submit\"]"), n = document.querySelector("[data-workflow-new], [data-workflow-switch-confirm]");
		this.updateLive({
			project: e.dataset.project || "",
			session: e.dataset.session || "",
			instance: e.dataset.instance || "",
			title: e.querySelector("[data-live-title]")?.textContent || "New conversation",
			renameAvailable: !!t,
			renameDisabled: !t || t.disabled,
			newDisabled: !!n?.disabled
		});
	};
	updateLive = (e) => {
		if (!this.mounted || JSON.stringify(this.snapshot.live) === JSON.stringify(e) || e && (!e.project || !e.session || !e.instance || !this.snapshot.groups.has(e.project))) return;
		let t = this.snapshot.live;
		if (this.publish({ live: e }), !e) return;
		let n = this.snapshot.groups.get(e.project), r = n.rows.some((t) => t.session_id === e.session) ? n.rows.map((t) => t.session_id === e.session ? {
			...t,
			name: e.title
		} : t) : [{
			session_id: e.session,
			name: e.title
		}, ...n.rows].slice(0, gl.rows), i = n.instance !== e.instance;
		i && (this.requests.get(e.project)?.abort(), this.requests.delete(e.project)), this.group(e.project, {
			rows: r,
			activeSession: e.session,
			...i ? {
				instance: e.instance,
				deleteSupported: !1,
				loaded: !1,
				loading: !1
			} : {}
		}), i && t && n.expanded && !this.snapshot.hideSessions && this.load(e.project);
	};
	preemptInventory = () => {
		for (let [e, t] of this.requests) t.abort(), this.group(e, {
			loading: !1,
			deleteSupported: !1,
			loaded: !1
		});
		this.requests.clear();
	};
	async load(e, t = 0) {
		let n = this.snapshot.groups.get(e);
		if (!this.mounted || !n || n.loading) return;
		if (this.reads >= gl.reads || n.pages >= gl.pages) {
			this.group(e, {
				truncated: !0,
				error: "Session inventory read limit reached. Reload to refresh."
			});
			return;
		}
		this.requests.get(e)?.abort();
		let r = new AbortController(), i = this.owner, a = n.instance;
		this.requests.set(e, r), this.reads++, this.group(e, {
			loading: !0,
			error: "",
			pages: n.pages + 1
		});
		let o = () => this.mounted && this.owner === i && this.requests.get(e) === r, s = setTimeout(() => r.abort(), 1e4);
		try {
			let n = await fetch(`/projects/${encodeURIComponent(e)}/sidebar-sessions?offset=${t}`, {
				credentials: "same-origin",
				signal: r.signal,
				headers: { Accept: "application/json" }
			});
			if (!n.ok) throw Error("Inventory unavailable");
			let i = JSON.parse(await wl(n, gl.response));
			if (!o()) return;
			let s = Cl(i, e, t, a, this.snapshot.live), c = new Map((t ? this.snapshot.groups.get(e).rows : []).map((e) => [e.session_id, e]));
			s.rows.forEach((e) => c.set(e.session_id, e));
			let l = this.snapshot.live;
			l?.project === e && c.set(l.session, {
				session_id: l.session,
				name: l.title
			});
			let u = [...c.values()];
			l?.project === e && u.length > gl.rows && (u = [c.get(l.session), ...u.filter((e) => e.session_id !== l.session)]), this.group(e, {
				...s,
				rows: u.slice(0, gl.rows),
				loaded: !0,
				loading: !1
			});
		} catch {
			o() && this.group(e, {
				error: "Sessions could not be loaded.",
				deleteSupported: !1,
				loading: !1
			});
		} finally {
			clearTimeout(s), o() && this.requests.delete(e);
		}
	}
	toggle(e) {
		let t = this.snapshot.groups.get(e);
		t && (this.group(e, { expanded: !t.expanded }), !t.expanded && !t.loaded && !this.snapshot.hideSessions && this.load(e));
	}
	syncVisibility = (e) => {
		if (this.publish({ hideSessions: e }), !e) for (let e of this.snapshot.groups.values()) e.expanded && !e.loaded && !e.loading && this.load(e.project);
	};
	invalidate = (e) => {
		this.requests.get(e)?.abort(), this.requests.delete(e), this.group(e, {
			loading: !1,
			loaded: !1,
			deleteSupported: !1
		}), this.snapshot.groups.get(e)?.expanded && !this.snapshot.hideSessions && this.load(e);
	};
	deleted = (e, t) => {
		let n = this.snapshot.groups.get(e);
		n && (this.group(e, { rows: n.rows.filter((e) => e.session_id !== t) }), this.invalidate(e));
	};
	authority(e, t) {
		let n = this.snapshot.groups.get(e), r = this.snapshot.live;
		if (n && n.rows.some((e) => e.session_id === t)) return {
			supported: n.deleteSupported,
			available: n.loaded && n.available,
			active: n.activeSession === t || r?.project === e && r.session === t,
			instance: n.instance
		};
	}
	eligible(e) {
		let t = this.authority(e.project, e.session), n = e.trigger.dataset;
		return this.mounted && e.owner === this.owner && e.trigger.isConnected && n.project === e.project && n.session === e.session && n.instance === e.instance && n.deleteSupported === "true" && n.deleteAvailable === "true" && n.deleteActive === "false" && El(t) && t?.instance === e.instance;
	}
	closeMenu = (e = !0) => {
		let t = this.snapshot.menu?.trigger;
		this.publish({ menu: null }), e && t?.isConnected && t.focus({ preventScroll: !0 });
	};
	openDelete(e) {
		if (!this.eligible(e) || this.snapshot.deletion?.pending) return;
		let t = this.snapshot.groups.get(e.project)?.rows.find((t) => t.session_id === e.session)?.name || "Untitled session";
		this.closeMenu(!1), this.publish({ deletion: {
			...e,
			name: t,
			pending: !1,
			submitted: !1,
			error: ""
		} });
	}
	closeDelete = () => {
		let e = this.snapshot.deletion;
		e && !e.pending && (this.publish({ deletion: null }), queueMicrotask(() => {
			let t = e.trigger, n = t.closest("[data-shell-session]")?.querySelector("a"), r = [...document.querySelectorAll("[data-sidebar-project]")].find((t) => t.dataset.sidebarProject === e.project)?.querySelector("[data-workspace-toggle]");
			(t.isConnected ? !t.hidden && !t.disabled ? t : n : r)?.focus({ preventScroll: !0 });
		}));
	};
	async remove(e) {
		let t = this.snapshot.deletion, n = this.snapshot.bootstrap?.csrf;
		if (!t || !e || !n || t.pending || t.submitted) return;
		if (!this.eligible(t)) {
			this.closeDelete();
			return;
		}
		t.pending = !0, t.submitted = !0, this.publish({ deletion: { ...t } });
		let r = new AbortController(), i = setTimeout(() => r.abort(), 15e3), a = "Deletion could not be confirmed. It may have completed. Refresh the session list before trying again; no automatic retry was sent.";
		try {
			let e = await fetch(`/projects/${encodeURIComponent(t.project)}/sessions/${encodeURIComponent(t.session)}/delete`, {
				method: "POST",
				credentials: "same-origin",
				signal: r.signal,
				headers: {
					Accept: "application/json",
					"Content-Type": "application/x-www-form-urlencoded"
				},
				body: new URLSearchParams({
					csrf: n,
					confirm: "delete",
					instance_id: t.instance
				})
			}), i = await wl(e, 65536);
			if (!e.ok) throw [
				400,
				401,
				403,
				404,
				409
			].includes(e.status) && e.headers.get("content-type")?.startsWith("text/plain") && i.length <= 1024 && (a = i.trim() + " No automatic retry was sent."), Error("Deletion not confirmed");
			let o = JSON.parse(i);
			if (o.project_id !== t.project || o.session_id !== t.session || o.instance_id !== t.instance || o.deleted !== !0) throw Error("Invalid receipt");
			if (document.dispatchEvent(new CustomEvent("snow:session-deleted", { detail: {
				project: t.project,
				session: t.session,
				instance: t.instance
			} })), !this.mounted) return;
			if (this.owner !== t.owner) {
				this.snapshot.groups.get(t.project)?.instance === t.instance && this.deleted(t.project, t.session);
				return;
			}
			this.deleted(t.project, t.session), this.publish({ deletion: {
				...t,
				pending: !1
			} }), this.closeDelete();
			let s = this.snapshot.bootstrap;
			if (!this.snapshot.live && s?.project === t.project && s.session === t.session) {
				let e = [...document.querySelectorAll("[data-sidebar-project]")].find((e) => e.dataset.sidebarProject === t.project)?.querySelector("[data-shell-project-new]");
				e?.isConnected && await this.navigate("/?" + new URLSearchParams({
					view: "projects",
					project: t.project,
					new: "1"
				}), e);
			}
		} catch {
			this.mounted && this.owner === t.owner && (this.invalidate(t.project), this.publish({ deletion: {
				...t,
				pending: !1,
				error: a
			} }));
		} finally {
			clearTimeout(i);
		}
	}
	collapse = () => {
		if (this.snapshot.narrow) return;
		let e = !this.snapshot.collapsed;
		this.publish({ collapsed: e }), this.command({
			type: "collapse",
			collapsed: e
		});
	};
	navigation = (e) => {
		e &&= this.snapshot.narrow, this.publish({ navOpen: e }), this.command({
			type: "navigation",
			open: e
		});
	};
	setTheme = (e) => {
		this.publish({ theme: e });
	};
	theme(e) {
		this.setTheme(e), this.command({
			type: "theme",
			theme: e
		});
	}
	openWorkspacePicker = (e) => {
		if (this.snapshot.menu?.trigger === e) {
			this.closeMenu();
			return;
		}
		this.publish({ menu: {
			kind: "workspace",
			project: "",
			session: "",
			instance: "",
			trigger: e,
			owner: this.owner
		} });
	};
	openSettings(e, t) {
		this.closeMenu(!1), this.publish({
			settings: e,
			settingsReturn: t
		});
	}
	closeSettings = () => {
		let e = this.snapshot.settingsReturn;
		this.publish({ settings: null }), queueMicrotask(() => {
			e?.isConnected && e.focus({ preventScroll: !0 });
		});
	};
}(), Ol = Object.freeze({
	initialize: U.initialize,
	syncVisibility: U.syncVisibility,
	syncCurrent: U.syncCurrent,
	invalidate: U.invalidate,
	deleted: U.deleted,
	preemptInventory: U.preemptInventory
}), kl = Object.freeze({
	renderRow(e, t) {
		let n = U.snapshot.groups.get(t.project);
		e.isConnected && e.dataset.shellSession === t.session && n && n.instance === t.instance && U.group(t.project, {
			available: n.available && t.available,
			deleteSupported: n.deleteSupported && t.supported,
			activeSession: t.active ? t.session : n.activeSession
		});
	},
	appendMenu() {}
});
//#endregion
//#region src/shell/Icons.tsx
function Al({ name: e, className: t = "icon" }) {
	return /* @__PURE__ */ (0, D.jsx)("svg", {
		className: t,
		viewBox: "0 0 24 24",
		fill: "none",
		stroke: "currentColor",
		strokeWidth: "1.6",
		strokeLinecap: "round",
		strokeLinejoin: "round",
		"aria-hidden": "true",
		children: /* @__PURE__ */ (0, D.jsx)("path", { d: e === "snow" ? "M12 2v20M3.34 7l17.32 10M3.34 17 20.66 7m-12-3.5L12 6l3.34-2.5M8.66 20.5 12 18l3.34 2.5M3.8 10.9l3.8.4L8 7.5m8 9 .4-3.8 3.8.4M3.8 13.1l3.8-.4.4 3.8m8-9 .4 3.8 3.8-.4" : {
			panel: "M9 3v18M5 3h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2Z",
			close: "m6 6 12 12M6 18 18 6",
			chat: "M21 11.5a8.5 8.5 0 0 1-8.5 8.5H4l-3 2 1.5-6A8.5 8.5 0 1 1 21 11.5Z",
			new: "M13 20H4l-3 2 1.5-6A8.5 8.5 0 1 1 21 11M18 15v6m-3-3h6",
			folder: "M3 7V5a2 2 0 0 1 2-2h5l2 3h7a2 2 0 0 1 2 2v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7Z",
			search: "m16 16 5 5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0Z",
			list: "M9 6h12M9 12h12M9 18h12M3 6h1M3 12h1M3 18h1",
			plus: "M12 5v14M5 12h14",
			chevron: "m9 5 7 7-7 7",
			check: "m5 12 4 4L19 6",
			settings: "m9 3-1 3-3 1-2 5 2 5 3 1 1 3h6l1-3 3-1 2-5-2-5-3-1-1-3ZM16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z",
			activity: "M2 12h5l3-8 4 16 3-8h5",
			light: "M12 2v2m0 16v2M2 12h2m16 0h2M5 5l1.5 1.5m11 11L19 19M5 19l1.5-1.5m11-11L19 5M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z",
			dark: "M20.5 14A8.5 8.5 0 0 1 10 3.5 8.5 8.5 0 1 0 20.5 14Z"
		}[e] })
	});
}
//#endregion
//#region src/shell/menu-aria.ts
var jl = "snow-shell-menu";
function Ml(e, t, n = "", r = "", i = !0) {
	let a = i && e?.kind === t && e.project === n && e.session === r;
	return {
		"aria-expanded": !!a,
		"aria-controls": a ? jl : void 0
	};
}
//#endregion
//#region src/shell/Sidebar.tsx
function Nl(e) {
	return e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey;
}
function Pl({ href: e, navigate: t, children: n, ...r }) {
	return /* @__PURE__ */ (0, D.jsx)("a", {
		...r,
		href: e,
		"data-snow-navigation": "",
		onClick: (n) => {
			Nl(n) && (n.preventDefault(), n.stopPropagation(), t(e, n.currentTarget));
		},
		children: n
	});
}
function Fl({ c: e, group: t, row: n }) {
	let r = (0, l.useRef)(null), i = (0, l.useRef)(null), a = (0, l.useRef)(!1), o = e.snapshot.live, s = o?.project === t.project && o.session === n.session_id, c = e.authority(t.project, n.session_id)?.active || !1, u = s ? o.title || "New conversation" : n.name || "Untitled session", d = s && o.renameAvailable, f = t.deleteSupported || d, p = e.snapshot.menu, m = p?.kind === "session" && p.project === t.project && p.session === n.session_id;
	(0, l.useLayoutEffect)(() => {
		if (!f) {
			let t = document.activeElement === i.current || m || a.current && document.activeElement === document.body;
			a.current = !1, m && e.closeMenu(!1), t && r.current?.focus({ preventScroll: !0 });
		}
	}, [
		e,
		f,
		m
	]);
	let h = s || !o && e.snapshot.bootstrap?.project === t.project && e.snapshot.bootstrap.session === n.session_id, g = o?.project === t.project ? o.instance : t.instance;
	return /* @__PURE__ */ (0, D.jsxs)("div", {
		className: "shell-session-row",
		"data-shell-session": n.session_id,
		"data-shell-live-session": s ? "" : void 0,
		children: [/* @__PURE__ */ (0, D.jsxs)("a", {
			ref: r,
			href: Tl(t.project, { session: n.session_id }),
			"data-snow-navigation": "",
			"data-shell-session-open": "",
			"data-project": t.project,
			"data-instance": g,
			"aria-current": h ? "page" : void 0,
			onClick: (r) => {
				Nl(r) && (r.preventDefault(), r.stopPropagation(), e.navigation(!1), g ? document.dispatchEvent(new CustomEvent("snow:session-select", { detail: {
					project: t.project,
					session: n.session_id,
					instance: g,
					trigger: r.currentTarget
				} })) : document.dispatchEvent(new CustomEvent("snow:session-resume", {
					cancelable: !0,
					detail: {
						project: t.project,
						session: n.session_id,
						trigger: r.currentTarget
					}
				})) && e.navigate(r.currentTarget.href, r.currentTarget));
			},
			children: [/* @__PURE__ */ (0, D.jsx)(Al, { name: "chat" }), /* @__PURE__ */ (0, D.jsx)("span", {
				title: u,
				children: u
			})]
		}), /* @__PURE__ */ (0, D.jsx)("button", {
			ref: i,
			type: "button",
			onFocus: () => {
				a.current = !0;
			},
			onBlur: (e) => {
				e.currentTarget.hidden || (a.current = !1);
			},
			className: "quiet shell-session-more",
			"data-shell-session-menu": "",
			hidden: !f,
			disabled: !f || !t.deleteSupported && !!d && !!o?.renameDisabled,
			"data-project": t.project,
			"data-session": n.session_id,
			"data-instance": t.instance,
			"data-session-name": u,
			"data-delete-supported": String(t.deleteSupported),
			"data-delete-available": String(t.loaded && t.available),
			"data-delete-active": String(c),
			"aria-label": `Session actions for ${u}`,
			title: "Session actions",
			"aria-haspopup": "menu",
			...Ml(p, "session", t.project, n.session_id, !!f),
			onClick: (r) => {
				r.stopPropagation(), f && e.publish({ menu: {
					kind: "session",
					project: t.project,
					session: n.session_id,
					instance: s ? o.instance : t.instance,
					trigger: r.currentTarget,
					owner: e.owner
				} });
			},
			children: "⋯"
		})]
	});
}
function Il({ c: e, project: t, group: n, hidden: r }) {
	let i = e.snapshot.bootstrap?.project === t.id, a = (0, l.useRef)(null), o = (0, l.useRef)(null), s = !!n.error || n.loaded && !n.available, c = !n.loading && !s && n.hasMore && n.rows.length < 100, u = (0, l.useRef)(!1), d = s || c || n.loading && u.current;
	(0, l.useLayoutEffect)(() => {
		u.current && !d && document.activeElement === a.current && o.current?.focus({ preventScroll: !0 }), u.current = d;
	}, [d]);
	let f = n.loading ? "Loading sessions…" : n.error || (n.loaded && !n.available ? "Saved sessions are unavailable." : n.loaded && !n.rows.length ? "No saved sessions" : (n.truncated || n.hasMore) && !c ? "Showing a limited session list" : "");
	return /* @__PURE__ */ (0, D.jsxs)("div", {
		className: "project-group",
		"data-sidebar-project": t.id,
		hidden: r,
		children: [/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "shell-project-row",
			children: [
				/* @__PURE__ */ (0, D.jsx)("button", {
					ref: o,
					type: "button",
					className: "quiet shell-project-action workspace-disclosure",
					"data-workspace-toggle": "",
					"aria-label": `Sessions in ${t.name}`,
					"aria-expanded": n.expanded,
					"aria-controls": `workspace-sessions-${t.id}`,
					onClick: (n) => {
						n.stopPropagation(), e.toggle(t.id);
					},
					children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "chevron" })
				}),
				/* @__PURE__ */ (0, D.jsxs)(Pl, {
					className: `project-link${i ? " selected" : ""}`,
					href: Tl(t.id),
					navigate: (n, r) => {
						if (e.navigation(!1), e.snapshot.live?.project !== t.id) return e.navigate(n, r);
					},
					title: t.path,
					"aria-current": i ? "page" : void 0,
					children: [/* @__PURE__ */ (0, D.jsx)("span", {
						className: "folder-icon",
						"aria-hidden": "true",
						children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "folder" })
					}), /* @__PURE__ */ (0, D.jsxs)("span", {
						className: "project-link-text",
						children: [
							/* @__PURE__ */ (0, D.jsx)("strong", { children: t.name }),
							/* @__PURE__ */ (0, D.jsx)("span", {
								className: "project-link-path",
								children: t.path
							}),
							!t.available && /* @__PURE__ */ (0, D.jsx)("span", {
								className: "unavailable",
								children: "Folder unavailable"
							})
						]
					})]
				}),
				/* @__PURE__ */ (0, D.jsxs)("span", {
					className: "shell-project-actions",
					children: [/* @__PURE__ */ (0, D.jsx)("a", {
						className: "quiet shell-project-action",
						"data-shell-project-new": "",
						href: Tl(t.id, { new: "1" }),
						"data-snow-navigation": "",
						"aria-label": `New session in ${t.name}`,
						title: `New session in ${t.name}`,
						"aria-disabled": e.snapshot.live?.project === t.id && e.snapshot.live.newDisabled ? "true" : void 0,
						onClick: (n) => {
							Nl(n) && (n.preventDefault(), n.stopPropagation(), !(e.snapshot.live?.project === t.id && e.snapshot.live.newDisabled) && (e.preemptInventory(), e.navigation(!1), document.dispatchEvent(new CustomEvent("snow:session-new", { detail: {
								project: t.id,
								trigger: n.currentTarget
							} }))));
						},
						children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "new" })
					}), /* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "quiet shell-project-action",
						"data-shell-project-menu": "",
						"aria-label": `Workspace actions for ${t.name}`,
						"aria-haspopup": "menu",
						...Ml(e.snapshot.menu, "project", t.id),
						title: "Workspace actions",
						onClick: (n) => {
							n.stopPropagation(), e.publish({ menu: {
								kind: "project",
								project: t.id,
								session: "",
								instance: "",
								trigger: n.currentTarget,
								owner: e.owner
							} });
						},
						children: "⋯"
					})]
				})
			]
		}), /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "session-tree",
			id: `workspace-sessions-${t.id}`,
			"data-workspace-sessions": "",
			hidden: !n.expanded || e.snapshot.hideSessions,
			"aria-busy": n.loading || void 0,
			children: [
				n.rows.map((t) => /* @__PURE__ */ (0, D.jsx)(Fl, {
					c: e,
					group: n,
					row: t
				}, t.session_id)),
				f && /* @__PURE__ */ (0, D.jsx)("p", {
					"data-sidebar-session-status": "",
					role: "status",
					children: f
				}),
				/* @__PURE__ */ (0, D.jsx)("button", {
					ref: a,
					type: "button",
					className: "quiet",
					"data-sidebar-session-more": "",
					"data-offset": c ? n.nextOffset : 0,
					hidden: !d,
					"aria-disabled": n.loading || void 0,
					onClick: (r) => {
						r.stopPropagation(), n.loading || e.load(t.id, c ? n.nextOffset : 0);
					},
					children: c ? "Load more" : "Retry"
				})
			]
		})]
	});
}
function Ll({ controller: e, view: t }) {
	let n = (0, l.useRef)(null), r = t.bootstrap, i = (0, l.useRef)(t.searchOpen);
	if ((0, l.useLayoutEffect)(() => {
		e.restoreSidebar();
	}, [e]), (0, l.useLayoutEffect)(() => {
		t.searchOpen && !i.current && n.current?.focus(), i.current = t.searchOpen;
	}, [t.searchOpen]), !r) return null;
	let a = t.query.trim().toLocaleLowerCase(), o = (e) => (!a || e.name.toLocaleLowerCase().includes(a) || e.path.toLocaleLowerCase().includes(a)) && (!t.pinnedOnly || e.pinned), s = (t, n) => (e.navigation(!1), e.navigate(t, n));
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("aside", {
		id: "project-navigation",
		className: `sidebar${t.navOpen ? " nav-open" : ""}`,
		"aria-label": "Projects and workspace",
		inert: t.narrow && !t.navOpen,
		role: t.navOpen ? "dialog" : void 0,
		"aria-modal": t.navOpen ? !0 : void 0,
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "sidebar-brand-row",
				children: [
					/* @__PURE__ */ (0, D.jsxs)(Pl, {
						href: "/",
						navigate: s,
						className: "brand",
						"aria-label": "Snow workspace home",
						children: [
							/* @__PURE__ */ (0, D.jsx)("span", {
								className: "brand-mark",
								"aria-hidden": "true",
								children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "snow" })
							}),
							/* @__PURE__ */ (0, D.jsx)("span", {
								className: "brand-name",
								children: "snow"
							}),
							/* @__PURE__ */ (0, D.jsx)("span", {
								className: "brand-label",
								children: "WORKSPACE"
							})
						]
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						className: "quiet sidebar-collapse",
						type: "button",
						"data-sidebar-collapse": "",
						"aria-label": t.collapsed ? "Expand sidebar" : "Collapse sidebar",
						"aria-controls": "project-navigation",
						"aria-expanded": !t.collapsed,
						onClick: (t) => {
							t.stopPropagation(), e.collapse();
						},
						children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "panel" })
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						className: "quiet mobile-nav-close",
						type: "button",
						"data-nav-close": "",
						"aria-label": "Close project navigation",
						onClick: (t) => {
							t.stopPropagation(), e.navigation(!1);
						},
						children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "close" })
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsxs)("a", {
				className: "sidebar-new-session",
				href: r.project ? Tl(r.project, { new: "1" }) : "/",
				"data-snow-navigation": "",
				"data-shell-new-session": "",
				"aria-label": "New session",
				"aria-disabled": t.live?.newDisabled || void 0,
				onClick: (n) => {
					Nl(n) && (n.preventDefault(), n.stopPropagation(), !t.live?.newDisabled && (e.preemptInventory(), e.navigation(!1), r.project ? document.dispatchEvent(new CustomEvent("snow:session-new", { detail: {
						project: r.project,
						trigger: n.currentTarget
					} })) : s("/", n.currentTarget)));
				},
				children: [/* @__PURE__ */ (0, D.jsx)(Al, { name: "new" }), /* @__PURE__ */ (0, D.jsx)("span", { children: "New session" })]
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "sidebar-heading",
				children: [/* @__PURE__ */ (0, D.jsx)("span", {
					className: "sidebar-title",
					children: "Workspaces"
				}), /* @__PURE__ */ (0, D.jsxs)("div", {
					className: "sidebar-tools",
					children: [
						/* @__PURE__ */ (0, D.jsx)("button", {
							className: "quiet",
							type: "button",
							"data-sidebar-search-toggle": "",
							"aria-label": "Search workspaces",
							"aria-controls": "sidebar-search",
							"aria-expanded": t.searchOpen,
							onClick: (n) => {
								n.stopPropagation(), t.collapsed && e.collapse(), e.publish({
									searchOpen: !t.searchOpen,
									query: t.searchOpen ? "" : t.query
								});
							},
							children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "search" })
						}),
						/* @__PURE__ */ (0, D.jsx)("button", {
							className: "quiet",
							type: "button",
							"data-sidebar-view-toggle": "",
							"aria-label": "Workspace view options",
							"aria-haspopup": "menu",
							...Ml(t.menu, "view"),
							onClick: (t) => {
								t.stopPropagation(), e.publish({ menu: {
									kind: "view",
									project: "",
									session: "",
									instance: "",
									trigger: t.currentTarget,
									owner: e.owner
								} });
							},
							children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "list" })
						}),
						/* @__PURE__ */ (0, D.jsx)(Pl, {
							className: "quiet add-project-link",
							href: "/?view=projects#add-project",
							navigate: s,
							"aria-label": "Add workspace",
							children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "plus" })
						})
					]
				})]
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				id: "sidebar-search",
				className: "sidebar-search",
				hidden: !t.searchOpen,
				children: [/* @__PURE__ */ (0, D.jsx)("label", {
					className: "visually-hidden",
					htmlFor: "workspace-search",
					children: "Search registered workspaces"
				}), /* @__PURE__ */ (0, D.jsx)("input", {
					ref: n,
					id: "workspace-search",
					type: "search",
					placeholder: "Search workspaces…",
					autoComplete: "off",
					maxLength: 200,
					"data-sidebar-search": "",
					value: t.query,
					onChange: (t) => e.publish({ query: t.currentTarget.value })
				})]
			}),
			/* @__PURE__ */ (0, D.jsxs)("nav", {
				className: "project-tree",
				"aria-label": "Project navigation",
				children: [
					r.projects.map((n) => /* @__PURE__ */ (0, D.jsx)(Il, {
						c: e,
						project: n,
						group: t.groups.get(n.id),
						hidden: !o(n)
					}, n.id)),
					!r.projects.length && /* @__PURE__ */ (0, D.jsxs)("div", {
						className: "sidebar-empty",
						children: [/* @__PURE__ */ (0, D.jsx)("p", { children: "No workspaces yet" }), /* @__PURE__ */ (0, D.jsx)("span", {
							className: "fine",
							children: "Add a folder to get started."
						})]
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						className: "sidebar-search-empty fine",
						"data-sidebar-search-empty": "",
						hidden: !a && !t.pinnedOnly || r.projects.some(o),
						children: "No matching workspaces."
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "sidebar-bottom",
				children: [
					/* @__PURE__ */ (0, D.jsxs)(Pl, {
						className: "sidebar-utility",
						href: "/?view=activity",
						navigate: s,
						"aria-label": "Activity & attention",
						title: "Activity & attention",
						"aria-current": r.view === "activity" ? "page" : void 0,
						children: [/* @__PURE__ */ (0, D.jsx)(Al, { name: "activity" }), /* @__PURE__ */ (0, D.jsx)("span", { children: "Activity & attention" })]
					}),
					/* @__PURE__ */ (0, D.jsxs)(Pl, {
						className: "sidebar-utility",
						href: "/?view=organization",
						navigate: s,
						"aria-label": "Organize workspaces",
						title: "Organize workspaces",
						"aria-current": r.view === "organization" ? "page" : void 0,
						children: [/* @__PURE__ */ (0, D.jsx)(Al, { name: "folder" }), /* @__PURE__ */ (0, D.jsx)("span", { children: "Organize workspaces" })]
					}),
					/* @__PURE__ */ (0, D.jsx)("div", {
						className: "sidebar-settings",
						children: /* @__PURE__ */ (0, D.jsxs)("button", {
							className: "sidebar-settings-trigger",
							type: "button",
							"data-settings-open": "",
							"aria-label": "Settings",
							"aria-haspopup": "dialog",
							"aria-controls": "settings-dialog",
							onClick: (t) => {
								t.stopPropagation(), e.openSettings("general", t.currentTarget);
							},
							children: [/* @__PURE__ */ (0, D.jsx)(Al, { name: "settings" }), /* @__PURE__ */ (0, D.jsx)("span", { children: "Settings" })]
						})
					})
				]
			})
		]
	}), /* @__PURE__ */ (0, D.jsx)("button", {
		className: "nav-backdrop",
		type: "button",
		"data-nav-close": "",
		"aria-label": "Close project navigation",
		tabIndex: -1,
		hidden: !t.navOpen,
		onClick: (t) => {
			t.stopPropagation(), e.navigation(!1);
		}
	})] });
}
//#endregion
//#region src/shell/Settings.tsx
var Rl = ({ value: e }) => /* @__PURE__ */ (0, D.jsx)("input", {
	type: "hidden",
	name: "csrf",
	value: e
}), zl = (e) => e.networkProfile === "trusted-lan-http" ? "Direct HTTP on this host’s private LAN address. Traffic is unencrypted; use only on a trusted LAN." : "Direct numeric-loopback HTTP. Remote access is disabled.";
function Bl({ data: e }) {
	return /* @__PURE__ */ (0, D.jsxs)("div", {
		className: "settings-content",
		children: [
			/* @__PURE__ */ (0, D.jsx)(Re, { csrf: e.csrf }),
			/* @__PURE__ */ (0, D.jsxs)("section", {
				className: "settings-panel",
				children: [
					/* @__PURE__ */ (0, D.jsx)("h2", { children: "Pair another browser" }),
					/* @__PURE__ */ (0, D.jsx)("p", { children: "Rotate the reusable pairing code, then open this host's Snow URL in the other browser. The replacement code lasts up to 30 days and survives Snow restarts. Rotation invalidates the previous code, not browsers already paired." }),
					/* @__PURE__ */ (0, D.jsxs)("form", {
						method: "post",
						action: "/access/pair",
						children: [/* @__PURE__ */ (0, D.jsx)(Rl, { value: e.csrf }), /* @__PURE__ */ (0, D.jsx)("button", {
							className: "primary",
							type: "submit",
							children: "Rotate pairing code"
						})]
					}),
					e.pairingCode && /* @__PURE__ */ (0, D.jsxs)("div", {
						className: "pairing-result",
						role: "status",
						children: [
							/* @__PURE__ */ (0, D.jsx)("label", {
								htmlFor: "pairing-code",
								children: "Reusable · expires in 30 days"
							}),
							/* @__PURE__ */ (0, D.jsx)("input", {
								id: "pairing-code",
								type: "text",
								value: e.pairingCode,
								readOnly: !0,
								autoComplete: "off",
								spellCheck: !1
							}),
							/* @__PURE__ */ (0, D.jsx)("p", {
								className: "fine",
								children: "Treat this code as a credential. It is never placed in a URL or saved in browser storage."
							})
						]
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsxs)("section", {
				className: "settings-panel",
				children: [/* @__PURE__ */ (0, D.jsx)("h2", { children: "Access boundaries" }), /* @__PURE__ */ (0, D.jsxs)("dl", { children: [
					/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Network" }), /* @__PURE__ */ (0, D.jsx)("dd", { children: zl(e) })] }),
					/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Browser lifetime" }), /* @__PURE__ */ (0, D.jsx)("dd", { children: "Paired browsers stay connected for up to 30 days. Up to 8 browsers may be paired." })] }),
					/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Restart behavior" }), /* @__PURE__ */ (0, D.jsx)("dd", { children: "Pairing survives Snow restarts. Signing out revokes this browser's access." })] }),
					/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("dt", { children: "Host authority" }), /* @__PURE__ */ (0, D.jsx)("dd", { children: "Snow has no process sandbox. Agent workers run with the host user's privileges." })] })
				] })]
			}),
			/* @__PURE__ */ (0, D.jsxs)("section", {
				className: "settings-panel",
				children: [
					/* @__PURE__ */ (0, D.jsx)("h2", { children: "Revoke all browsers" }),
					/* @__PURE__ */ (0, D.jsx)("p", { children: "Sign out every paired browser, including this one, and rotate the pairing code. Restart Snow to see the replacement code in the terminal." }),
					/* @__PURE__ */ (0, D.jsxs)("form", {
						method: "post",
						action: "/access/revoke-all",
						children: [
							/* @__PURE__ */ (0, D.jsx)(Rl, { value: e.csrf }),
							/* @__PURE__ */ (0, D.jsxs)("label", {
								className: "checkbox-label",
								children: [/* @__PURE__ */ (0, D.jsx)("input", {
									type: "checkbox",
									name: "confirm",
									value: "revoke",
									required: !0
								}), " I understand all browsers will need to pair again."]
							}),
							/* @__PURE__ */ (0, D.jsx)("button", {
								className: "button danger",
								type: "submit",
								children: "Revoke all browser access"
							})
						]
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsxs)("section", {
				className: "settings-panel",
				children: [
					/* @__PURE__ */ (0, D.jsx)("h2", { children: "This browser" }),
					/* @__PURE__ */ (0, D.jsx)("p", { children: "Sign out to revoke this browser’s access. Other paired browsers stay connected." }),
					/* @__PURE__ */ (0, D.jsxs)("form", {
						method: "post",
						action: "/logout",
						children: [/* @__PURE__ */ (0, D.jsx)(Rl, { value: e.csrf }), /* @__PURE__ */ (0, D.jsx)("button", {
							className: "button",
							type: "submit",
							children: "Sign out"
						})]
					})
				]
			})
		]
	});
}
function Vl({ controller: e, view: t }) {
	let n = (0, l.useRef)(null), r = (0, l.useRef)(null), i = (0, l.useRef)(null), a = t.bootstrap;
	if ((0, l.useLayoutEffect)(() => {
		let e = n.current;
		e && (t.settings && !e.open ? (e.returnValue = "", e.showModal(), i.current?.focus()) : !t.settings && e.open && e.close());
	}, [!!t.settings]), (0, l.useLayoutEffect)(() => {
		r.current && (r.current.scrollTop = 0);
	}, [t.settings]), (0, l.useLayoutEffect)(() => () => {
		n.current?.open && n.current.close();
	}, []), !a) return null;
	let o = (t, n) => (e.closeSettings(), e.navigation(!1), e.navigate(t, n));
	return /* @__PURE__ */ (0, D.jsx)("dialog", {
		ref: n,
		id: "settings-dialog",
		className: "settings-dialog",
		"aria-labelledby": "settings-title",
		onCancel: (t) => {
			t.preventDefault(), e.closeSettings();
		},
		onClick: (t) => {
			if (t.target !== t.currentTarget) return;
			let n = t.currentTarget.getBoundingClientRect();
			(t.clientX < n.left || t.clientX > n.right || t.clientY < n.top || t.clientY > n.bottom) && e.closeSettings();
		},
		children: /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "settings-layout",
			children: [/* @__PURE__ */ (0, D.jsxs)("nav", {
				className: "settings-nav",
				"aria-label": "Settings sections",
				children: [/* @__PURE__ */ (0, D.jsx)("h2", {
					id: "settings-title",
					children: "Settings"
				}), /* @__PURE__ */ (0, D.jsx)("div", {
					className: "settings-nav-list",
					children: [
						"general",
						"workspaces",
						"access"
					].map((n) => /* @__PURE__ */ (0, D.jsxs)("button", {
						type: "button",
						"data-settings-section": n,
						"aria-controls": `settings-${n}`,
						"aria-current": t.settings === n ? "true" : void 0,
						onClick: (t) => {
							t.stopPropagation(), e.publish({ settings: n });
						},
						children: [/* @__PURE__ */ (0, D.jsx)(Al, { name: n === "general" ? "settings" : n === "workspaces" ? "folder" : "panel" }), /* @__PURE__ */ (0, D.jsx)("span", { children: n === "access" ? "Browser access" : n === "general" ? "General" : "Workspaces" })]
					}, n))
				})]
			}), /* @__PURE__ */ (0, D.jsxs)("div", {
				className: "settings-column",
				children: [/* @__PURE__ */ (0, D.jsx)("header", {
					className: "settings-header",
					children: /* @__PURE__ */ (0, D.jsx)("button", {
						ref: i,
						type: "button",
						className: "settings-close",
						"data-settings-close": "",
						"aria-label": "Close Settings",
						autoFocus: !0,
						onClick: (t) => {
							t.stopPropagation(), e.closeSettings();
						},
						children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "close" })
					})
				}), /* @__PURE__ */ (0, D.jsxs)("div", {
					className: "settings-options",
					ref: r,
					children: [
						/* @__PURE__ */ (0, D.jsxs)("section", {
							id: "settings-general",
							"data-settings-panel": "general",
							"aria-labelledby": "settings-general-title",
							hidden: t.settings !== "general",
							children: [
								/* @__PURE__ */ (0, D.jsx)("h3", {
									id: "settings-general-title",
									className: "settings-section-title",
									children: "General"
								}),
								/* @__PURE__ */ (0, D.jsxs)("div", {
									className: "settings-group",
									children: [
										/* @__PURE__ */ (0, D.jsx)("h4", { children: "Appearance" }),
										/* @__PURE__ */ (0, D.jsx)("div", {
											className: "settings-appearance",
											role: "group",
											"aria-label": "Appearance",
											children: ["light", "dark"].map((n) => /* @__PURE__ */ (0, D.jsxs)("button", {
												type: "button",
												"data-theme-choice": n,
												"aria-pressed": t.theme === n,
												onClick: (t) => {
													t.stopPropagation(), e.theme(n);
												},
												children: [/* @__PURE__ */ (0, D.jsx)(Al, {
													name: n,
													className: "appearance-icon"
												}), n === "light" ? "Light" : "Dark"]
											}, n))
										}),
										/* @__PURE__ */ (0, D.jsx)("p", {
											className: "fine",
											children: "Saved in this browser. Does not change the host’s terminal theme."
										})
									]
								}),
								/* @__PURE__ */ (0, D.jsx)(tt, {
									csrf: a.csrf,
									enabled: a.hostSettingsEnabled,
									projects: a.projects.map(({ id: e, name: t }) => ({
										id: e,
										name: t
									}))
								}),
								/* @__PURE__ */ (0, D.jsxs)("div", {
									className: "settings-group",
									children: [
										/* @__PURE__ */ (0, D.jsx)("h4", { children: "On your machine" }),
										/* @__PURE__ */ (0, D.jsx)("p", { children: "Snow runs tools with your host account’s privileges, not in a process sandbox. Opening Settings does not start a worker or contact a model provider." }),
										/* @__PURE__ */ (0, D.jsx)("p", { children: "Live browser conversations require explicit project activation and use permission prompts. Provider configuration, plugins and host permissions are managed on the host, not here." }),
										/* @__PURE__ */ (0, D.jsx)("p", {
											className: "fine",
											children: /* @__PURE__ */ (0, D.jsx)("a", {
												href: "/static/HARNESS-NOTICE.txt",
												children: "Third-party notices"
											})
										}),
										/* @__PURE__ */ (0, D.jsxs)("div", {
											className: "settings-host",
											children: [
												/* @__PURE__ */ (0, D.jsx)("span", { className: "status-dot" }),
												"Direct HTTP connection",
												/* @__PURE__ */ (0, D.jsxs)("span", {
													className: "version",
													children: ["Snow ", a.version]
												})
											]
										})
									]
								})
							]
						}),
						/* @__PURE__ */ (0, D.jsxs)("section", {
							id: "settings-workspaces",
							"data-settings-panel": "workspaces",
							"aria-labelledby": "settings-workspaces-title",
							hidden: t.settings !== "workspaces",
							children: [
								/* @__PURE__ */ (0, D.jsx)("h3", {
									id: "settings-workspaces-title",
									className: "settings-section-title",
									children: "Workspaces"
								}),
								/* @__PURE__ */ (0, D.jsx)("p", { children: "Registered folders on this host. Choosing a workspace only opens its saved view; it does not activate an agent." }),
								/* @__PURE__ */ (0, D.jsxs)("nav", {
									className: "settings-workspace-list",
									"aria-label": "Registered workspaces",
									children: [a.projects.map((e) => /* @__PURE__ */ (0, D.jsxs)(Pl, {
										href: Tl(e.id),
										navigate: o,
										children: [
											/* @__PURE__ */ (0, D.jsx)(Al, { name: "folder" }),
											/* @__PURE__ */ (0, D.jsxs)("span", {
												title: `${e.name} · ${e.path}`,
												children: [
													/* @__PURE__ */ (0, D.jsx)("strong", { children: e.name }),
													/* @__PURE__ */ (0, D.jsx)("small", { children: e.path }),
													!e.available && /* @__PURE__ */ (0, D.jsx)("small", { children: "Folder unavailable" })
												]
											}),
											/* @__PURE__ */ (0, D.jsx)(Al, { name: "chevron" })
										]
									}, e.id)), !a.projects.length && /* @__PURE__ */ (0, D.jsx)("p", {
										className: "fine",
										children: "No workspaces registered yet."
									})]
								}),
								/* @__PURE__ */ (0, D.jsxs)("section", {
									className: "settings-group project-trust-settings",
									"aria-labelledby": "project-trust-heading",
									children: [
										/* @__PURE__ */ (0, D.jsx)("h4", {
											id: "project-trust-heading",
											children: "Remembered project trust"
										}),
										/* @__PURE__ */ (0, D.jsx)("p", { children: "Trusted projects still need an explicit Start or Resume. Forgetting trust restores confirmation on the next activation; it does not stop a running worker or change tool permissions." }),
										/* @__PURE__ */ (0, D.jsx)("ul", {
											className: "project-trust-list",
											children: a.projects.filter((e) => e.trustRemembered).map((e) => /* @__PURE__ */ (0, D.jsxs)("li", { children: [/* @__PURE__ */ (0, D.jsxs)("span", { children: [/* @__PURE__ */ (0, D.jsx)("strong", { children: e.name }), /* @__PURE__ */ (0, D.jsx)("small", { children: e.available ? "Trust remembered" : "Folder unavailable · confirmation required" })] }), /* @__PURE__ */ (0, D.jsxs)("form", {
												method: "post",
												action: `/projects/${encodeURIComponent(e.id)}/trust/revoke`,
												"data-project-trust-revoke": "",
												children: [
													/* @__PURE__ */ (0, D.jsx)(Rl, { value: a.csrf }),
													/* @__PURE__ */ (0, D.jsx)("input", {
														type: "hidden",
														name: "confirm",
														value: "revoke"
													}),
													/* @__PURE__ */ (0, D.jsx)("button", {
														type: "submit",
														className: "button quiet",
														"aria-label": `Forget trust for ${e.name}`,
														children: "Forget trust"
													})
												]
											})] }, e.id))
										}),
										/* @__PURE__ */ (0, D.jsx)("p", {
											className: "fine project-trust-empty",
											hidden: a.projects.some((e) => e.trustRemembered),
											children: "No remembered project trust. Projects ask for confirmation before their first activation."
										})
									]
								}),
								/* @__PURE__ */ (0, D.jsxs)("section", {
									className: "settings-group",
									"aria-labelledby": "workspace-skills-title",
									children: [
										/* @__PURE__ */ (0, D.jsx)("h4", {
											id: "workspace-skills-title",
											children: "Installed skills"
										}),
										/* @__PURE__ */ (0, D.jsx)("p", { children: "Enabled by default for new workspaces and remembered per project for future starts across this manager’s paired browsers. Disable a workspace here when you do not want its installed skills available. Changes do not affect a running worker or grant project trust or tool permissions." }),
										/* @__PURE__ */ (0, D.jsx)("ul", {
											className: "project-trust-list",
											children: a.projects.map((e) => /* @__PURE__ */ (0, D.jsxs)("li", { children: [/* @__PURE__ */ (0, D.jsxs)("span", { children: [/* @__PURE__ */ (0, D.jsx)("strong", { children: e.name }), /* @__PURE__ */ (0, D.jsxs)("small", { children: [e.skillsEnabled ? "Enabled for future starts" : "Disabled for future starts", !e.available && " · Folder unavailable"] })] }), /* @__PURE__ */ (0, D.jsxs)("form", {
												method: "post",
												action: `/projects/${encodeURIComponent(e.id)}/skills`,
												children: [
													/* @__PURE__ */ (0, D.jsx)(Rl, { value: a.csrf }),
													/* @__PURE__ */ (0, D.jsx)("input", {
														type: "hidden",
														name: "enable_skills",
														value: e.skillsEnabled ? "" : "runtime"
													}),
													/* @__PURE__ */ (0, D.jsx)("button", {
														type: "submit",
														className: "button quiet",
														"aria-label": `${e.skillsEnabled ? "Disable" : "Enable"} installed skills for ${e.name}`,
														disabled: !e.available,
														children: e.skillsEnabled ? "Disable skills" : "Enable skills"
													})
												]
											})] }, e.id))
										}),
										!a.projects.length && /* @__PURE__ */ (0, D.jsx)("p", {
											className: "fine",
											children: "Add a workspace to choose its startup preference."
										})
									]
								}),
								/* @__PURE__ */ (0, D.jsxs)("div", {
									className: "settings-workspace-actions",
									children: [/* @__PURE__ */ (0, D.jsxs)(Pl, {
										className: "button",
										href: "/?view=projects#add-project",
										navigate: o,
										children: [/* @__PURE__ */ (0, D.jsx)(Al, { name: "plus" }), "Add workspace"]
									}), /* @__PURE__ */ (0, D.jsx)(Pl, {
										className: "button quiet",
										href: "/?view=projects",
										navigate: o,
										children: "Manage workspaces"
									})]
								}),
								/* @__PURE__ */ (0, D.jsx)("p", {
									className: "fine",
									children: "Add workspace uses the existing host folder picker. Folder paths refer to the Snow host, not this browser’s device."
								})
							]
						}),
						/* @__PURE__ */ (0, D.jsxs)("section", {
							id: "settings-access",
							"data-settings-panel": "access",
							"aria-labelledby": "settings-access-title",
							hidden: t.settings !== "access",
							children: [/* @__PURE__ */ (0, D.jsx)("h3", {
								id: "settings-access-title",
								className: "settings-section-title",
								children: "Browser access"
							}), a.view === "access" ? /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("p", { children: "Browser access controls are open in the workspace behind Settings." }), /* @__PURE__ */ (0, D.jsx)("button", {
								className: "button",
								type: "button",
								"data-settings-close": "",
								"data-settings-access-return": "",
								onClick: (t) => {
									t.stopPropagation(), e.publish({ settingsReturn: document.getElementById("workspace-content") }), e.closeSettings();
								},
								children: "Show browser access controls"
							})] }) : /* @__PURE__ */ (0, D.jsx)(Bl, { data: a })]
						})
					]
				})]
			})]
		})
	});
}
//#endregion
//#region src/shell/Menu.tsx
function Hl() {
	return window.SnowMenus;
}
function Ul({ controller: e, menu: t }) {
	let n = (0, l.useRef)(null), [r] = (0, l.useState)(() => {
		let e = document.createElement("div");
		return e.id = jl, e;
	});
	(0, l.useLayoutEffect)(() => {
		r.classList.toggle("shell-workspace-menu", t.kind === "workspace"), r.setAttribute("aria-label", t.kind === "session" ? "Session actions" : t.kind === "project" ? "Workspace actions" : t.kind === "workspace" ? "Choose workspace" : "Workspace view options");
		let n = Hl();
		if (!n || !t.trigger.isConnected || e.owner !== t.owner) {
			e.closeMenu(!1);
			return;
		}
		return n.open({
			trigger: t.trigger,
			panel: r,
			managedTrigger: !0,
			placement: t.kind === "workspace" ? "top-start" : "bottom-start",
			onClose: () => {
				e.snapshot.menu === t && e.closeMenu(!1);
			}
		}), () => {
			r.isConnected && n.close({ restoreFocus: !1 });
		};
	}, [
		e,
		t,
		r
	]), (0, l.useLayoutEffect)(() => {
		let e = n.current;
		e && r.isConnected && (!e.isConnected || e instanceof HTMLButtonElement && e.disabled) && (document.activeElement === document.body || document.activeElement === e) && (r.querySelector("[role=menuitem]:not(:disabled)") || r).focus({ preventScroll: !0 });
	});
	let i = e.snapshot.live, a = t.project === i?.project && t.session === i.session, o = e.authority(t.project, t.session), s = () => e.owner === t.owner && t.trigger.isConnected && e.snapshot.menu === t, c = e.snapshot.bootstrap?.projects.find((e) => e.id === t.project);
	return (0, u.createPortal)(/* @__PURE__ */ (0, D.jsxs)("div", {
		className: "snow-menu-content",
		tabIndex: -1,
		onFocusCapture: (e) => {
			n.current = e.target;
		},
		children: [
			t.kind === "session" && /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [a && i?.renameAvailable && /* @__PURE__ */ (0, D.jsx)("button", {
				type: "button",
				className: "snow-menu-row",
				role: "menuitem",
				disabled: i.renameDisabled,
				onClick: () => {
					s() && e.snapshot.live?.instance === t.instance && e.snapshot.live.session === t.session && e.snapshot.live.project === t.project && (e.closeMenu(!1), window.SnowConversation?.rename(t.trigger));
				},
				children: "Rename"
			}), o?.supported && /* @__PURE__ */ (0, D.jsx)("button", {
				type: "button",
				className: "snow-menu-row danger",
				role: "menuitem",
				"data-session-delete": "",
				disabled: !El(o),
				title: o.active ? "Switch to another session or close this workspace before deleting the active session" : void 0,
				onClick: () => {
					s() && e.openDelete(t);
				},
				children: "Delete session"
			})] }),
			t.kind === "project" && c && /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "shell-workspace-menu-heading",
				children: [/* @__PURE__ */ (0, D.jsx)("strong", { children: c.name }), /* @__PURE__ */ (0, D.jsx)("small", { children: c.path })]
			}), [!1, !0].map((n) => {
				let r = Tl(c.id, {
					inspect: "project",
					...e.snapshot.bootstrap?.project === c.id && e.snapshot.bootstrap.session ? { session: e.snapshot.bootstrap.session } : {}
				}) + (n ? "#remove-project" : "");
				return /* @__PURE__ */ (0, D.jsx)("a", {
					className: "snow-menu-row",
					role: "menuitem",
					href: r,
					"data-snow-navigation": "",
					onClick: (i) => {
						if (i.ctrlKey || i.metaKey || i.shiftKey || i.altKey || i.button !== 0 || (i.preventDefault(), i.stopPropagation(), !s())) return;
						let a = document.getElementById("project-inspector")?.dataset.project === c.id;
						e.closeMenu(!1), a ? document.dispatchEvent(new CustomEvent("snow:inspect-project", { detail: {
							project: c.id,
							remove: n,
							trigger: t.trigger
						} })) : (e.navigation(!1), e.navigate(r, i.currentTarget));
					},
					children: n ? "Remove registration…" : "Workspace settings…"
				}, String(n));
			})] }),
			t.kind === "workspace" && /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
				/* @__PURE__ */ (0, D.jsx)("span", {
					className: "picker-heading",
					children: "Workspaces"
				}),
				e.snapshot.bootstrap?.projects.map((t) => /* @__PURE__ */ (0, D.jsx)("a", {
					className: "snow-menu-row",
					role: "menuitem",
					"data-home-project": t.id,
					"data-home-project-name": t.name,
					"data-home-project-available": String(t.available),
					href: Tl(t.id),
					"data-snow-navigation": "",
					onClick: (n) => {
						n.defaultPrevented || n.ctrlKey || n.metaKey || n.shiftKey || n.altKey || n.button !== 0 || (n.preventDefault(), s() && (e.closeMenu(!1), e.navigate(Tl(t.id), n.currentTarget)));
					},
					children: /* @__PURE__ */ (0, D.jsxs)("span", {
						title: `${t.name} · ${t.path}`,
						children: [
							/* @__PURE__ */ (0, D.jsx)("strong", { children: t.name }),
							/* @__PURE__ */ (0, D.jsx)("small", { children: t.path }),
							!t.available && /* @__PURE__ */ (0, D.jsx)("small", { children: "Folder unavailable" })
						]
					})
				}, t.id)),
				!e.snapshot.bootstrap?.projects.length && /* @__PURE__ */ (0, D.jsx)("p", {
					className: "fine",
					children: "No workspaces registered yet."
				}),
				/* @__PURE__ */ (0, D.jsx)("a", {
					className: "snow-menu-row picker-add",
					role: "menuitem",
					href: "/?view=projects#add-project",
					"data-snow-navigation": "",
					onClick: (t) => {
						t.ctrlKey || t.metaKey || t.shiftKey || t.altKey || t.button !== 0 || (t.preventDefault(), s() && (e.closeMenu(!1), e.navigate("/?view=projects#add-project", t.currentTarget)));
					},
					children: "Add workspace"
				})
			] }),
			t.kind === "view" && /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("button", {
				type: "button",
				className: "snow-menu-row",
				role: "menuitemcheckbox",
				"aria-checked": !e.snapshot.hideSessions,
				onClick: () => {
					s() && e.syncVisibility(!e.snapshot.hideSessions);
				},
				children: [/* @__PURE__ */ (0, D.jsx)("span", {
					className: "snow-menu-row-label",
					children: "Show saved sessions"
				}), /* @__PURE__ */ (0, D.jsx)("span", {
					className: "snow-menu-check",
					"aria-hidden": "true",
					children: /* @__PURE__ */ (0, D.jsx)("span", {
						style: { visibility: e.snapshot.hideSessions ? "hidden" : "visible" },
						children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "check" })
					})
				})]
			}), /* @__PURE__ */ (0, D.jsxs)("button", {
				type: "button",
				className: "snow-menu-row",
				role: "menuitemcheckbox",
				"aria-checked": e.snapshot.pinnedOnly,
				onClick: () => {
					s() && e.publish({ pinnedOnly: !e.snapshot.pinnedOnly });
				},
				children: [/* @__PURE__ */ (0, D.jsx)("span", {
					className: "snow-menu-row-label",
					children: "Pinned workspaces only"
				}), /* @__PURE__ */ (0, D.jsx)("span", {
					className: "snow-menu-check",
					"aria-hidden": "true",
					children: /* @__PURE__ */ (0, D.jsx)("span", {
						style: { visibility: e.snapshot.pinnedOnly ? "visible" : "hidden" },
						children: /* @__PURE__ */ (0, D.jsx)(Al, { name: "check" })
					})
				})]
			})] })
		]
	}), r);
}
//#endregion
//#region src/shell/DeleteDialog.tsx
function Wl({ controller: e }) {
	let t = e.snapshot.deletion, n = (0, l.useRef)(null), [r, i] = (0, l.useState)(!1);
	return (0, l.useLayoutEffect)(() => {
		let e = n.current;
		if (e && t) return e.open || e.showModal(), () => {
			e.open && e.close();
		};
	}, [!!t]), (0, l.useLayoutEffect)(() => {
		i(!1);
	}, [
		t?.owner,
		t?.project,
		t?.session
	]), (0, l.useLayoutEffect)(() => {
		t && !t.pending && !t.submitted && !e.eligible(t) && e.closeDelete();
	}), t ? /* @__PURE__ */ (0, D.jsxs)("dialog", {
		id: "session-delete-dialog",
		className: "folder-dialog session-delete-dialog",
		ref: n,
		"aria-labelledby": "session-delete-title",
		"aria-describedby": "session-delete-warning",
		onCancel: (t) => {
			t.preventDefault(), e.closeDelete();
		},
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-heading",
				children: [/* @__PURE__ */ (0, D.jsx)("h2", {
					id: "session-delete-title",
					children: "Delete session?"
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "quiet",
					"data-delete-cancel": "",
					disabled: t.pending,
					onClick: e.closeDelete,
					children: t.submitted ? "Close" : "Cancel"
				})]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "session-delete-name",
				"data-delete-name": "",
				children: t.name
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				id: "session-delete-warning",
				children: "Permanently delete this saved conversation and its managed private session data. This cannot be undone. Workspace files are not deleted. Unsent drafts remain in this tab."
			}),
			/* @__PURE__ */ (0, D.jsxs)("label", {
				className: "checkbox-label",
				children: [/* @__PURE__ */ (0, D.jsx)("input", {
					type: "checkbox",
					"data-delete-confirm": "",
					checked: r,
					disabled: t.submitted,
					onChange: (e) => i(e.currentTarget.checked)
				}), /* @__PURE__ */ (0, D.jsx)("span", { children: "I understand this permanently deletes the saved session." })]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "error",
				"data-delete-error": "",
				role: "alert",
				hidden: !t.error,
				children: t.error
			}),
			/* @__PURE__ */ (0, D.jsx)("div", {
				className: "dialog-actions",
				children: /* @__PURE__ */ (0, D.jsx)("button", {
					type: "button",
					className: "button danger",
					"data-delete-submit": "",
					disabled: !r || t.submitted || !e.eligible(t),
					onClick: () => void e.remove(r),
					children: t.pending ? "Deleting…" : "Delete session"
				})
			})
		]
	}) : null;
}
//#endregion
//#region src/shell/ReactShell.tsx
function Gl({ bootstrap: e, navigationHost: t, navigate: n, command: r }) {
	let i = (0, l.useSyncExternalStore)(U.subscribe, U.getSnapshot, U.getSnapshot);
	if ((0, l.useLayoutEffect)(() => (U.mount(e, n, r), () => U.dispose()), [e]), (0, l.useLayoutEffect)(() => {
		let e = matchMedia("(max-width: 767px)"), t = () => {
			U.publish({ narrow: e.matches }), !e.matches && U.snapshot.navOpen && U.navigation(!1);
		};
		return t(), e.addEventListener("change", t), () => e.removeEventListener("change", t);
	}, []), (0, l.useLayoutEffect)(() => {
		n && (U.navigate = n), r && (U.command = r);
	}, [n, r]), !i.bootstrap) return null;
	let a = /* @__PURE__ */ (0, D.jsx)(Ll, {
		controller: U,
		view: i
	});
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		t ? (0, u.createPortal)(a, t) : a,
		/* @__PURE__ */ (0, D.jsx)(Vl, {
			controller: U,
			view: i
		}),
		i.menu && /* @__PURE__ */ (0, D.jsx)(Ul, {
			controller: U,
			menu: i.menu
		}),
		/* @__PURE__ */ (0, D.jsx)(Wl, { controller: U })
	] });
}
//#endregion
//#region src/workspace/model.ts
var Kl = (e) => typeof e == "string" && /^[a-f0-9]{8}-(?:[a-f0-9]{4}-){3}[a-f0-9]{12}$/.test(e), ql = (e) => new TextEncoder().encode(e).length;
function Jl(e) {
	if (!e || typeof e != "object" || Array.isArray(e)) throw Error("Invalid presentation");
	return e;
}
function Yl(e, t) {
	if (typeof e != "string" || ql(e) > t) throw Error("Invalid text");
	return e;
}
function Xl(e) {
	if (typeof e != "boolean") throw Error("Invalid flag");
	return e;
}
function Zl(e) {
	let t = Jl(e);
	if (!Kl(t.id)) throw Error("Invalid project");
	return {
		id: t.id,
		name: Yl(t.name, 128),
		path: Yl(t.path, 4096),
		available: Xl(t.available),
		trusted: Xl(t.trusted),
		skillsEnabled: Xl(t.skillsEnabled)
	};
}
function Ql(e) {
	let t = Jl(e);
	if (!Array.isArray(t.projects) || t.projects.length > 100) throw Error("Invalid projects");
	let n = t.projects.map(Zl);
	if (new Set(n.map((e) => e.id)).size !== n.length) throw Error("Duplicate project");
	return {
		projects: n,
		error: Yl(t.error, 4096)
	};
}
function $l(e) {
	let t = Jl(e), n = Yl(t.networkProfile, 32);
	if (n !== "local" && n !== "trusted-lan-http") throw Error("Invalid network profile");
	return {
		csrf: Yl(t.csrf, 512),
		error: Yl(t.error, 4096),
		networkProfile: n
	};
}
function eu(e) {
	let t = Jl(e);
	return {
		...Ql(t),
		csrf: Yl(t.csrf, 512),
		registryEnabled: Xl(t.registryEnabled),
		projectOperationsEnabled: Xl(t.projectOperationsEnabled)
	};
}
var tu = (e) => "/?view=projects&project=" + encodeURIComponent(e);
function nu(e) {
	let t = Yl(e, 8192);
	if (!t) return t;
	if (!t.startsWith("/?") || /[\u0000-\u0020\\]/u.test(t)) throw Error("Invalid navigation");
	let n = new URL(t, "http://snow.invalid"), r = n.searchParams;
	if (n.hash || r.get("view") !== "projects" || [...r.keys()].some((e) => ![
		"view",
		"project",
		"session",
		"offset"
	].includes(e) || r.getAll(e).length !== 1) || r.has("session") && !/^[A-Za-z0-9_-]{1,128}$/.test(r.get("session")) || r.has("offset") && (!/^(0|[1-9][0-9]{0,4})$/.test(r.get("offset")) || Number(r.get("offset")) > 1e4)) throw Error("Invalid navigation");
	return t;
}
function ru(e) {
	let t = Jl(e), n = Zl(t.project), r = Yl(t.sessionID, 128);
	if (r && !/^[A-Za-z0-9_-]{1,128}$/.test(r)) throw Error("Invalid session");
	let i = nu(t.nextURL), a = nu(t.recoveryURL);
	for (let e of [i, a]) if (e && new URL(e, "http://snow.invalid").searchParams.get("project") !== n.id) throw Error("Wrong project navigation");
	return {
		...$l(t),
		project: n,
		sessionID: r,
		sessionTitle: Yl(t.sessionTitle, 512),
		runtimeEnabled: Xl(t.runtimeEnabled),
		hasHistory: Xl(t.hasHistory),
		nextURL: i,
		recoveryURL: a,
		recoveryMessage: Yl(t.recoveryMessage, 4096)
	};
}
function iu(e, t) {
	return !Number.isFinite(e) || !Number.isFinite(t) ? 0 : Math.floor(Math.max(0, Math.min(e, t)));
}
//#endregion
//#region src/workspace/bridge.ts
var au = {
	text: "",
	workspaceText: "",
	homeEnabled: !1,
	workspaceEnabled: !1,
	name: "",
	pending: !1,
	privacy: "Choose a workspace, then start or resume. Your draft stays in this tab until you send it; reloading clears it.",
	notice: null
}, W = {
	path: "",
	parent: "",
	folders: [],
	hasMore: !1,
	busy: !1,
	canSelect: !1,
	error: "",
	status: ""
}, ou = {
	draft: au,
	folder: W,
	projectPath: "",
	flowError: "",
	activation: {
		busy: !1,
		error: ""
	},
	opening: {
		visible: !1,
		minHeight: 0
	},
	inspectorOpen: !1
}, su = /* @__PURE__ */ new Set(), cu = (e) => (su.add(e), () => {
	su.delete(e);
}), lu = () => ou;
function uu(e, t = !0) {
	ou = e;
	let n = () => su.forEach((e) => e());
	t ? (0, u.flushSync)(n) : n();
}
function du() {
	return (0, l.useSyncExternalStore)(cu, lu, lu);
}
function fu() {
	(0, l.useEffect)(() => {
		let e = !0;
		return queueMicrotask(() => {
			e && document.dispatchEvent(new Event("snow:workspace-mounted"));
		}), () => {
			e = !1;
		};
	}, []);
}
var pu = {
	updateDraft(e) {
		uu({
			...ou,
			draft: {
				...ou.draft,
				...e
			}
		});
	},
	editDraft(e, t = !1) {
		uu({
			...ou,
			draft: {
				...ou.draft,
				...t ? { workspaceText: e } : { text: e }
			}
		}, !1);
	},
	updateFolder(e) {
		uu({
			...ou,
			folder: {
				...ou.folder,
				...e
			}
		});
	},
	setProjectPath(e) {
		return !e.startsWith("/") || e.length > 4096 || /[\u0000-\u001f]/u.test(e) ? !1 : (uu({
			...ou,
			projectPath: e
		}), !0);
	},
	editProjectPath(e) {
		uu({
			...ou,
			projectPath: e
		}, !1);
	},
	updateActivation(e) {
		uu({
			...ou,
			activation: {
				...ou.activation,
				...e
			}
		});
	},
	updateOpening(e) {
		let t = typeof window > "u" ? 0 : window.innerHeight;
		uu({
			...ou,
			opening: {
				visible: e.visible === !0,
				minHeight: iu(e.minHeight, t)
			}
		});
	},
	updateInspector(e) {
		uu({
			...ou,
			inspectorOpen: e === !0
		});
	},
	updateFlowError(e) {
		uu({
			...ou,
			flowError: e
		});
	},
	reset() {
		uu({
			draft: au,
			folder: W,
			projectPath: "",
			flowError: "",
			activation: {
				busy: !1,
				error: ""
			},
			opening: {
				visible: !1,
				minHeight: 0
			},
			inspectorOpen: !1
		});
	}
};
//#endregion
//#region src/workspace/Icons.tsx
function mu({ kind: e }) {
	return /* @__PURE__ */ (0, D.jsx)("svg", {
		className: "icon",
		viewBox: "0 0 24 24",
		fill: "none",
		stroke: "currentColor",
		strokeWidth: "1.5",
		"aria-hidden": "true",
		children: e === "folder" ? /* @__PURE__ */ (0, D.jsx)("path", { d: "M3 7h6l2-3h5l2 3h3v13H3z" }) : e === "chevron" ? /* @__PURE__ */ (0, D.jsx)("path", { d: "m8 10 4 4 4-4" }) : e === "plus" ? /* @__PURE__ */ (0, D.jsx)("path", { d: "M12 5v14M5 12h14" }) : e === "close" ? /* @__PURE__ */ (0, D.jsx)("path", { d: "m6 6 12 12M6 18 18 6" }) : e === "panel" ? /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("rect", {
			x: "3",
			y: "4",
			width: "18",
			height: "16",
			rx: "2"
		}), /* @__PURE__ */ (0, D.jsx)("path", { d: "M9 4v16" })] }) : /* @__PURE__ */ (0, D.jsx)("path", { d: "M12 2v20M3.34 7l17.32 10M3.34 17 20.66 7m-12-3L12 7l3.34-3M4 10l4 1v4l-4 1m4.66 4L12 17l3.34 3M20 10l-4 1v4l4 1" })
	});
}
//#endregion
//#region src/workspace/Home.tsx
function hu({ projects: e, error: t }) {
	fu();
	let { draft: n } = du(), r = (0, l.useSyncExternalStore)(U.subscribe, U.getSnapshot, U.getSnapshot);
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)("header", {
		className: "workspace-heading home-heading",
		children: /* @__PURE__ */ (0, D.jsx)("button", {
			className: "quiet sidebar-restore",
			type: "button",
			"data-sidebar-restore": !0,
			"aria-label": "Expand sidebar",
			"aria-controls": "project-navigation",
			children: /* @__PURE__ */ (0, D.jsx)(mu, { kind: "panel" })
		})
	}), /* @__PURE__ */ (0, D.jsxs)("section", {
		className: "home-landing",
		"aria-labelledby": "home-title",
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "home-title",
				children: [
					/* @__PURE__ */ (0, D.jsx)("span", {
						className: "home-title-mark",
						"aria-hidden": "true",
						children: /* @__PURE__ */ (0, D.jsx)(mu, { kind: "snow" })
					}),
					/* @__PURE__ */ (0, D.jsx)("h1", {
						id: "home-title",
						children: "Into the Unknown"
					}),
					/* @__PURE__ */ (0, D.jsx)("span", {
						className: "preview-pill",
						children: "Preview"
					})
				]
			}),
			t && /* @__PURE__ */ (0, D.jsx)("p", {
				className: "error",
				role: "alert",
				children: t
			}),
			/* @__PURE__ */ (0, D.jsx)("div", {
				className: "home-controls",
				children: /* @__PURE__ */ (0, D.jsxs)("details", {
					className: "workspace-picker",
					children: [/* @__PURE__ */ (0, D.jsxs)("summary", {
						"aria-haspopup": "menu",
						...Ml(r.menu, "workspace"),
						children: [
							/* @__PURE__ */ (0, D.jsx)(mu, { kind: "folder" }),
							/* @__PURE__ */ (0, D.jsx)("span", {
								"data-home-workspace-label": !0,
								children: n.name || "Choose workspace"
							}),
							/* @__PURE__ */ (0, D.jsx)(mu, { kind: "chevron" })
						]
					}), /* @__PURE__ */ (0, D.jsxs)("div", {
						className: "workspace-picker-menu",
						children: [
							/* @__PURE__ */ (0, D.jsx)("span", {
								className: "picker-heading",
								children: "Workspaces"
							}),
							e.length ? e.map((e) => /* @__PURE__ */ (0, D.jsxs)("a", {
								"data-home-project": e.id,
								"data-home-project-name": e.name,
								"data-home-project-available": String(e.available),
								href: tu(e.id),
								"data-snow-navigation": "",
								children: [/* @__PURE__ */ (0, D.jsx)("span", {
									className: "folder-icon",
									children: /* @__PURE__ */ (0, D.jsx)(mu, { kind: "folder" })
								}), /* @__PURE__ */ (0, D.jsxs)("span", {
									title: `${e.name} · ${e.path}`,
									children: [
										/* @__PURE__ */ (0, D.jsx)("strong", { children: e.name }),
										/* @__PURE__ */ (0, D.jsx)("small", { children: e.path }),
										!e.available && /* @__PURE__ */ (0, D.jsx)("small", { children: "Folder unavailable" })
									]
								})]
							}, e.id)) : /* @__PURE__ */ (0, D.jsx)("p", {
								className: "fine",
								children: "No workspaces registered yet."
							}),
							/* @__PURE__ */ (0, D.jsx)("div", {
								className: "snow-menu-footer",
								children: /* @__PURE__ */ (0, D.jsxs)("a", {
									className: "picker-add",
									href: "/?view=projects#add-project",
									"data-snow-navigation": "",
									children: [/* @__PURE__ */ (0, D.jsx)(mu, { kind: "plus" }), /* @__PURE__ */ (0, D.jsx)("span", { children: "Add workspace" })]
								})
							})
						]
					})]
				})
			}),
			/* @__PURE__ */ (0, D.jsxs)("form", {
				id: "home-composer",
				className: "composer home-composer",
				"data-pending": n.pending ? "true" : void 0,
				children: [
					/* @__PURE__ */ (0, D.jsx)("label", {
						className: "visually-hidden",
						htmlFor: "home-prompt",
						children: "Draft a message to Snow"
					}),
					/* @__PURE__ */ (0, D.jsx)("textarea", {
						id: "home-prompt",
						rows: 2,
						maxLength: 65536,
						disabled: !n.homeEnabled,
						value: n.text,
						onChange: (e) => pu.editDraft(e.currentTarget.value),
						placeholder: "What would you like to work on?",
						"aria-describedby": "home-privacy"
					}),
					/* @__PURE__ */ (0, D.jsxs)("div", {
						className: "composer-actions home-composer-actions",
						children: [/* @__PURE__ */ (0, D.jsx)("span", {
							className: "home-composer-hint",
							children: "Draft first. Choose where to work."
						}), /* @__PURE__ */ (0, D.jsx)("button", {
							className: "home-send",
							type: "submit",
							disabled: !n.homeEnabled || n.pending,
							children: "Continue"
						})]
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				id: "home-privacy",
				className: "home-privacy fine",
				role: "status",
				children: n.privacy
			}),
			/* @__PURE__ */ (0, D.jsx)("noscript", { children: /* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				children: "Drafting needs JavaScript. Choose a workspace above to browse it."
			}) })
		]
	})] });
}
function gu() {
	let { draft: e } = du(), t = e.notice;
	return t ? /* @__PURE__ */ (0, D.jsxs)("aside", {
		className: "notice home-draft-notice",
		id: "home-draft-notice",
		children: [
			/* @__PURE__ */ (0, D.jsx)("p", { children: t.explanation }),
			/* @__PURE__ */ (0, D.jsx)("pre", {
				className: "home-draft-preview",
				children: t.text
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-actions",
				children: [
					t.useVisible && /* @__PURE__ */ (0, D.jsx)("button", {
						className: "button quiet",
						type: "button",
						"data-home-draft-use": !0,
						disabled: t.useDisabled,
						children: "Use draft"
					}),
					/* @__PURE__ */ (0, D.jsx)("a", {
						className: "button quiet",
						href: t.url,
						"data-snow-navigation": "",
						children: "Edit draft"
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						className: "button quiet",
						type: "button",
						"data-home-draft-discard": !0,
						children: "Discard draft"
					})
				]
			})
		]
	}) : null;
}
//#endregion
//#region src/workspace/operationModel.ts
var _u = (e, t) => typeof e == "string" && ql(e) <= t && !/[\u0000-\u001f\u007f-\u009f]/u.test(e), vu = (e) => _u(e, 4096) && e.startsWith("/"), yu = (e) => _u(e, 128) && e.length > 0 && e.trim() === e && e !== "." && e !== ".." && !/[\/\\]/u.test(e);
function bu(e) {
	if (!_u(e, 512) || !/^https:\/\/[a-zA-Z0-9.-]+\/[a-zA-Z0-9._/-]+$/.test(e)) return !1;
	let t = e.slice(e.indexOf("/", 8) + 1).split("/");
	return t.length >= 2 && t.length <= 8 && t.every((e) => e.length > 0 && e.length <= 128 && e !== "." && e !== "..");
}
var xu = {
	admitted: "Admitted",
	creating: "Creating destination",
	running: "Running",
	cancel_requested: "Stop requested — cleanup not yet confirmed",
	awaiting_registration: "Files retained — registration pending",
	succeeded: "Created and registered",
	failed: "Failed — destination may be partial",
	canceled: "Canceled after cleanup — files retained",
	interrupted: "Interrupted — ownership or outcome may be unknown",
	needs_review: "Needs review — do not assume completion"
}, Su = /* @__PURE__ */ new Set([
	"admitted",
	"creating",
	"running",
	"cancel_requested"
]), Cu = (e, t) => `${e === "/" ? "" : e}/${t}`;
function wu(e) {
	if (!e || typeof e != "object") return !1;
	let t = e;
	return vu(t.path) && typeof t.device == "string" && /^[0-9]{1,20}$/.test(t.device) && typeof t.inode == "string" && /^[0-9]{1,20}$/.test(t.inode);
}
function Tu(e) {
	let t = Jl(e);
	if (!Kl(t.id) || !["create", "clone"].includes(String(t.kind)) || !yu(t.name) || typeof t.state != "string" || !Object.hasOwn(xu, t.state) || !Number.isSafeInteger(t.revision) || Number(t.revision) < 1 || !wu(t.parent) || ![
		"observed",
		"not_observed",
		"unknown"
	].includes(String(t.outcome))) throw Error("Invalid operation");
	if (t.project_id && !Kl(t.project_id) || t.kind === "clone" && !bu(t.remote) || t.kind === "create" && t.remote) throw Error("Invalid remote or project");
	if (t.child !== void 0 && t.child !== null) {
		let e = Jl(t.child);
		if (e.path && (!wu(e) || e.path !== Cu(t.parent.path, t.name))) throw Error("Invalid child");
	}
	if ([
		"running",
		"succeeded",
		"awaiting_registration"
	].includes(t.state) && (!wu(t.child) || t.outcome !== "observed")) throw Error("Invalid observed identity");
	if (t.state === "succeeded" && !Kl(t.project_id) || t.state === "awaiting_registration" && t.project_id) throw Error("Invalid registration");
	if (![t.created_at, t.updated_at].every((e) => Number.isSafeInteger(e) && Number(e) > 0) || Number(t.updated_at) < Number(t.created_at)) throw Error("Invalid timestamp");
	return t;
}
function Eu(e, t) {
	let n = Jl(e);
	if (!Array.isArray(n.operations) || n.operations.length > 32) throw Error("Invalid inventory");
	let r = n.operations.map(Tu);
	if (new Set(r.map((e) => e.id)).size !== r.length || !Number.isSafeInteger(n.next_offset) || n.next_offset !== t + r.length || n.next_offset > 128 || typeof n.has_more != "boolean" || n.has_more && r.length === 0) throw Error("Invalid page");
	return {
		operations: r,
		next: n.next_offset,
		more: n.has_more
	};
}
function Du(e) {
	let t = Jl(e);
	if (!vu(t.path) || !vu(t.parent) || !Array.isArray(t.folders) || t.folders.length > 256 || !Number.isSafeInteger(t.next_offset) || Number(t.next_offset) < 0 || Number(t.next_offset) > 4096 || typeof t.has_more != "boolean") throw Error("Invalid folders");
	let n = t.folders.map((e) => {
		let t = Jl(e);
		if (!_u(t.name, 4096) || !vu(t.path)) throw Error("Invalid folder");
		return {
			name: t.name,
			path: t.path
		};
	});
	return {
		path: t.path,
		parent: t.parent,
		folders: n,
		next_offset: Number(t.next_offset),
		has_more: t.has_more,
		limited: t.limited === !0
	};
}
function Ou(e, t = Date.now()) {
	let n = Jl(e);
	if (!Kl(n.operation_id) || !vu(n.path) || !Number.isSafeInteger(n.expires_at) || Number(n.expires_at) <= t || Number(n.expires_at) > t + 301e3) throw Error("Invalid grant");
	return {
		operation_id: n.operation_id,
		path: n.path,
		expires_at: Number(n.expires_at)
	};
}
function ku(e, t, n) {
	let r = Tu(e);
	if (r.id !== t || n !== void 0 && r.revision <= n) throw Error("Wrong receipt");
	return r;
}
async function Au(e) {
	if (!e.ok) throw Error(e.status === 401 || e.status === 403 ? "auth" : e.status === 404 ? "missing" : "request");
	if (!e.body) throw Error("Empty response");
	let t = e.body.getReader(), n = [], r = 0;
	try {
		for (;;) {
			let { done: e, value: i } = await t.read();
			if (e) break;
			if (r += i.length, r > 65536) throw await t.cancel(), Error("Bounded response");
			n.push(i);
		}
	} finally {
		t.releaseLock();
	}
	let i = new Uint8Array(r), a = 0;
	for (let e of n) i.set(e, a), a += e.length;
	return JSON.parse(new TextDecoder("utf-8", { fatal: !0 }).decode(i));
}
//#endregion
//#region src/workspace/operationController.ts
var ju = /* @__PURE__ */ new Map();
function Mu(e) {
	let t = ju.get(e);
	if (!t) {
		if (ju.size >= 8) {
			let e = [...ju].find(([, e]) => !e.pending && !e.uncertain && !e.listeners.size);
			if (!e) throw Error("Too many pending authorization scopes");
			ju.delete(e[0]);
		}
		t = {
			draft: {
				parent: "",
				name: "",
				kind: "create",
				remote: ""
			},
			pending: !1,
			uncertain: "",
			listeners: /* @__PURE__ */ new Set()
		}, ju.set(e, t);
	}
	return t;
}
var Nu = class {
	scope;
	state;
	listeners = /* @__PURE__ */ new Set();
	reader = null;
	generation = 0;
	alive = !0;
	csrf;
	enabled;
	constructor(e, t) {
		this.csrf = e, this.enabled = t, this.scope = Mu(e), this.state = {
			draft: { ...this.scope.draft },
			reading: !1,
			pending: this.scope.pending,
			uncertain: this.scope.uncertain,
			grant: null,
			review: null,
			checked: !1,
			operations: [],
			folder: null,
			foldersOpen: !1,
			offset: 0,
			next: 0,
			more: !1,
			status: "No operation submitted. Closing this panel never cancels accepted work."
		}, this.scope.listeners.add(this.scopeChanged);
	}
	subscribe = (e) => (this.listeners.add(e), () => {
		this.listeners.delete(e);
	});
	getSnapshot = () => this.state;
	publish(e) {
		this.alive && (this.state = {
			...this.state,
			...e
		}, this.listeners.forEach((e) => e()));
	}
	scopeChanged = () => this.publish({
		pending: this.scope.pending,
		uncertain: this.scope.uncertain
	});
	syncScope() {
		this.scope.listeners.forEach((e) => e());
	}
	current(e) {
		return this.alive && e === this.generation;
	}
	canRead() {
		return this.alive && this.enabled && !this.scope.pending && !this.state.reading;
	}
	options(e, t) {
		return {
			credentials: "same-origin",
			cache: "no-store",
			redirect: "error",
			signal: e,
			headers: t ? {
				Accept: "application/json",
				"Content-Type": "application/x-www-form-urlencoded"
			} : { Accept: "application/json" },
			...t ? {
				method: "POST",
				body: new URLSearchParams({
					csrf: this.csrf,
					...t
				})
			} : {}
		};
	}
	saveDraft(e) {
		this.scope.draft = {
			...e,
			remote: bu(e.remote) ? e.remote : ""
		};
	}
	edit(e, t) {
		if (!this.alive || this.scope.pending || this.scope.uncertain || this.state.review || e === "kind" && t !== "create" && t !== "clone") return;
		let n = {
			...this.state.draft,
			[e]: t
		};
		this.saveDraft(n), this.publish({
			draft: n,
			review: null,
			checked: !1,
			...e === "parent" ? { grant: null } : {}
		});
	}
	setParentPath(e) {
		if (!vu(e) || !this.alive || this.scope.pending || this.scope.uncertain) return !1;
		this.stopRead();
		let t = {
			...this.state.draft,
			parent: e
		};
		return this.saveDraft(t), this.publish({
			draft: t,
			grant: null,
			review: null,
			checked: !1,
			foldersOpen: !1
		}), !0;
	}
	stopRead() {
		this.reader?.abort(), this.reader = null, this.generation++, this.publish({ reading: !1 });
	}
	retire() {
		this.stopRead(), this.saveDraft({
			...this.state.draft,
			remote: ""
		}), this.publish({
			draft: {
				...this.state.draft,
				remote: ""
			},
			grant: null,
			review: null,
			checked: !1,
			folder: null,
			foldersOpen: !1
		});
	}
	dispose() {
		this.alive && (this.retire(), this.alive = !1, this.scope.listeners.delete(this.scopeChanged), this.listeners.clear());
	}
	async read(e, t, n, r) {
		if (!this.canRead()) return;
		this.stopRead();
		let i = this.generation, a = new AbortController();
		this.reader = a, this.publish({
			reading: !0,
			review: null,
			checked: !1,
			status: n
		});
		try {
			let n = await Au(await fetch(e, this.options(AbortSignal.any([a.signal, AbortSignal.timeout(15e3)]), r)));
			this.current(i) && t(n);
		} catch (e) {
			this.current(i) && this.publish({ status: e instanceof Error && e.message === "auth" ? "Browser authorization changed. Pair or reload this manager before continuing." : "Could not read current host metadata. Refresh explicitly; no operation was retried." });
		} finally {
			this.current(i) && (this.reader = null, this.publish({ reading: !1 }));
		}
	}
	refresh = (e = 0) => this.read(`/operations?offset=${e}`, (t) => {
		let n = Eu(t, e);
		this.publish({
			...n,
			offset: e,
			status: "Read current durable operations. Refresh to observe changes; no work was replayed."
		});
	}, "Reading durable operation metadata…");
	browse = (e = "", t = 0) => {
		if (!(!this.canRead() || this.scope.uncertain || e !== "" && !vu(e))) return this.publish({ foldersOpen: !0 }), this.read("/projects/folders", (e) => {
			let t = Du(e);
			this.publish({
				folder: t,
				status: t.limited ? "Folder scan limit reached. Enter an absolute host path if needed." : "Folder browsing is read only. Use this path, then explicitly select the parent."
			});
		}, "Reading folders on the Snow host…", {
			path: e,
			offset: String(t)
		});
	};
	closeFolders() {
		this.stopRead(), this.publish({ foldersOpen: !1 });
	}
	select = async () => {
		if (!this.canRead() || this.scope.uncertain) return;
		let e = this.state.draft.parent;
		if (!vu(e)) {
			this.publish({ status: "Enter an absolute parent folder on the Snow host." });
			return;
		}
		this.publish({ grant: null }), await this.read("/projects/folders/select", (e) => {
			let t = Ou(e), n = {
				...this.state.draft,
				parent: t.path
			};
			this.saveDraft(n), this.publish({
				grant: t,
				draft: n,
				status: "Parent selected. Enter a new name, then review before any filesystem change."
			});
		}, "Validating the explicitly selected parent…", { path: e });
	};
	reviewCreation = () => {
		let { grant: e, draft: t } = this.state;
		if (!this.canRead() || this.scope.uncertain || !e) return;
		if (Date.now() >= e.expires_at) {
			this.publish({
				grant: null,
				status: "Parent selection expired. Explicitly select it again before reviewing."
			});
			return;
		}
		if (!yu(t.name) || t.kind === "clone" && !bu(t.remote)) {
			let e = {
				...t,
				remote: bu(t.remote) ? t.remote : ""
			};
			this.saveDraft(e), this.publish({
				draft: e,
				status: "Review requires one valid leaf name and, for cloning, an anonymous HTTPS URL. Rejected URLs are not retained."
			});
			return;
		}
		let n = t.kind === "clone", r = {
			operation_id: e.operation_id,
			name: t.name
		};
		n && (r.remote = t.remote), this.showReview({
			action: t.kind,
			id: e.operation_id,
			expires: e.expires_at,
			parentPath: e.path,
			request: r,
			title: n ? "Confirm repository clone" : "Confirm folder creation",
			detail: Cu(e.path, t.name) + (n ? `\nFrom ${t.remote}` : ""),
			effects: `${n ? "Create this destination and download the repository using anonymous HTTPS. " : "Create this empty destination. "}The host OS user's permissions apply; no startup-root confinement or disk quota is provided. Partial files are retained on failure or stop. Registration requires a separate explicit review after completion; no agent is activated. Closing this panel does not stop accepted work.`,
			button: n ? "Clone repository" : "Create folder"
		});
	};
	reviewAction = (e, t) => {
		if (!this.canRead() || this.scope.uncertain || this.state.review || !this.state.operations.some((t) => t.id === e.id && t.revision === e.revision)) return;
		let n = Su.has(e.state);
		if ((t === "cancel" ? !n || e.state === "cancel_requested" : n) || t === "register" && e.project_id) return;
		let r = t === "register" && (e.state !== "awaiting_registration" || e.outcome !== "observed"), i = {
			cancel: "Confirm stop request",
			reconcile: "Confirm identity observation",
			register: r ? "Review ordinary registration" : "Confirm retained registration",
			dismiss: "Confirm metadata-only dismissal"
		}, a = {
			cancel: "Request stop for this exact operation revision. Stop is not complete until worker cleanup is confirmed. Any destination and partial files remain; this is not rollback.",
			reconcile: "Observe the recorded directory identity only. This does not retry mkdir, clone, or registration, and does not stop any process.",
			register: r ? "Explicit ordinary registration of the directory currently at this destination. Its ownership or operation outcome is not established. This does not retry cloning and will not convert unknown or failed work into a created success. No agent is activated." : "Register only the exact retained child identity. Files and outcome are already recorded. This does not retry cloning, execute project code, or activate an agent.",
			dismiss: "Remove this settled operation's manager record only. No files, project registration, or session will be deleted. A dismissed request ID cannot be reused."
		}, o = { revision: String(e.revision) };
		r && (o.review = "true"), this.showReview({
			action: t,
			id: e.id,
			revision: e.revision,
			request: o,
			title: i[t],
			detail: `${e.name}\n${e.child?.path || Cu(e.parent.path, e.name)}\nReference ${e.id} · revision ${e.revision}`,
			effects: a[t],
			button: i[t]
		});
	};
	showReview(e) {
		this.publish({
			review: e,
			checked: !1
		});
	}
	back = () => {
		this.scope.pending || this.publish({
			review: null,
			checked: !1
		});
	};
	checkConsent = (e) => this.publish({ checked: e });
	confirm = async () => {
		let e = this.state.review;
		if (!e || !this.state.checked || !this.canRead() || this.scope.uncertain) return;
		let t = e.action === "create" || e.action === "clone";
		if (t && (!this.state.grant || Date.now() >= (e.expires || 0) || this.state.grant.operation_id !== e.id)) {
			this.publish({
				grant: null,
				review: null,
				checked: !1,
				status: "Parent selection expired. No operation was submitted; select and review again."
			});
			return;
		}
		this.stopRead();
		let n = this.generation;
		this.scope.pending = !0, this.scope.uncertain = e.id, this.scope.draft.remote = "", this.publish({
			review: null,
			checked: !1,
			...t ? { grant: null } : {},
			draft: {
				...this.state.draft,
				remote: ""
			},
			status: "Submitting the explicit request once. Closing this panel does not cancel accepted work."
		}), this.syncScope();
		try {
			let r = t ? `/projects/${e.action}` : `/operations/${e.id}/${e.action}`, i = await fetch(r, this.options(AbortSignal.timeout(15e3), e.request));
			if (e.action === "dismiss" && i.status === 204) {
				if (!this.current(n)) return;
				this.scope.uncertain = "", this.publish({
					operations: this.state.operations.filter((t) => t.id !== e.id),
					status: "Operation record dismissed. Files and project registration are unchanged."
				});
			} else {
				let r = await Au(i);
				if (!this.current(n)) return;
				let a = ku(r, e.id, e.revision);
				if (t && (a.kind !== e.action || a.name !== e.request.name || a.parent.path !== e.parentPath || (a.remote || "") !== (e.request.remote || ""))) throw Error("Wrong destination receipt");
				this.scope.uncertain = "";
				let o = this.state.operations.some((e) => e.id === a.id);
				this.publish({
					operations: o ? this.state.operations.map((e) => e.id === a.id ? a : e) : [a],
					...o ? {} : {
						offset: 0,
						more: !1
					},
					status: `${xu[a.state]}. Refresh to observe durable progress. No navigation or activation was performed.`
				});
			}
		} catch (e) {
			this.current(n) && this.publish({ status: e instanceof Error && e.message === "auth" ? "Browser authorization changed. The request may already have taken effect. Pair or reload, then inspect durable operations; never replay it." : "Could not confirm this request's outcome. Do not retry cloning or creation. Check its durable record; no mutation will be replayed automatically." });
		} finally {
			this.scope.pending = !1, this.syncScope();
		}
	};
	check = async () => {
		let e = this.scope.uncertain;
		if (!Kl(e) || !this.canRead()) return;
		this.stopRead();
		let t = this.generation, n = new AbortController();
		this.reader = n, this.publish({
			reading: !0,
			status: "Checking the existing request ID only; no operation is being retried…"
		});
		try {
			let r = await fetch(`/operations/${e}`, this.options(AbortSignal.any([n.signal, AbortSignal.timeout(15e3)])));
			if (!this.current(t)) return;
			if (r.status === 404) {
				this.scope.uncertain = "", this.publish({ status: "No retained record exists for that ID. Its grant cannot be reused. Inspect the host before explicitly selecting a parent for any new operation." });
				return;
			}
			let i = ku(await Au(r), e);
			if (!this.current(t)) return;
			this.scope.uncertain = "", this.publish({
				operations: [i],
				offset: 0,
				more: !1,
				status: `${xu[i.state]}. Existing record observed without retrying work.`
			});
		} catch {
			this.current(t) && this.publish({ status: "The request remains unconfirmed. Check again explicitly; never retry the clone to recover its status." });
		} finally {
			this.current(t) && (this.reader = null, this.publish({ reading: !1 })), this.syncScope();
		}
	};
}, Pu = /* @__PURE__ */ new WeakMap(), Fu = {
	init(e) {},
	setParentPath(e, t) {
		return Pu.get(e)?.setParentPath(t) || !1;
	},
	refresh(e) {
		return Pu.get(e)?.refresh(0);
	},
	dispose(e) {
		Pu.get(e)?.retire();
	}
};
function Iu({ csrf: e, enabled: t }) {
	return /* @__PURE__ */ (0, D.jsx)(Lu, {
		csrf: e,
		enabled: t
	}, e + String(t));
}
function Lu({ csrf: e, enabled: t }) {
	let [n] = (0, l.useState)(() => new Nu(e, t)), r = (0, l.useSyncExternalStore)(n.subscribe, n.getSnapshot, n.getSnapshot), i = (0, l.useRef)(null), a = (0, l.useRef)(null), o = (0, l.useRef)(null), s = (0, l.useRef)(null), c = (0, l.useRef)(null), u = (0, l.useRef)(!1), { draft: d, grant: f, review: p, folder: m } = r, h = r.pending || r.reading, g = !t || h, _ = g || !!p || !!r.uncertain;
	(0, l.useEffect)(() => {
		let e = i.current;
		if (!e) return;
		Pu.set(e, n);
		let t = () => n.retire(), r = e.closest("dialog");
		return r?.addEventListener("close", t), window.addEventListener("pagehide", t), () => {
			Pu.delete(e), r?.removeEventListener("close", t), window.removeEventListener("pagehide", t), n.dispose();
		};
	}, [n]), (0, l.useLayoutEffect)(() => {
		p && a.current?.focus();
	}, [p]);
	let v = (e, t) => n.edit(e, t);
	return /* @__PURE__ */ (0, D.jsxs)("details", {
		ref: i,
		className: "project-operations",
		"data-project-operations": !0,
		"data-enabled": String(t),
		"aria-busy": h,
		onToggle: (e) => {
			e.currentTarget.open || n.retire();
		},
		children: [/* @__PURE__ */ (0, D.jsx)("summary", { children: "Create / clone projects & operations" }), /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "project-operations-content",
			children: [
				/* @__PURE__ */ (0, D.jsx)("input", {
					type: "hidden",
					"data-op-csrf": !0,
					value: e
				}),
				/* @__PURE__ */ (0, D.jsx)("p", {
					className: "fine",
					children: "Create a new folder or clone an anonymous HTTPS repository on the Snow host. After completion, separately review and confirm registration. Registration never activates an agent or opens a conversation."
				}),
				/* @__PURE__ */ (0, D.jsx)("p", {
					className: "project-operations-authority",
					children: "Host-user OS authority: this is not confined to Snow’s startup folder or a filesystem sandbox. Creation writes a directory; cloning also accesses the network and downloads files. No disk quota is provided. SSH and embedded credentials are not supported."
				}),
				!t && /* @__PURE__ */ (0, D.jsx)("p", {
					className: "error",
					children: "Project operations are unavailable in this manager. No alternative command or activation will be attempted."
				}),
				/* @__PURE__ */ (0, D.jsxs)("fieldset", {
					className: "project-operation-draft",
					"data-op-draft": !0,
					disabled: _,
					onCompositionStart: () => {
						u.current = !0;
					},
					onCompositionEnd: () => {
						u.current = !1;
					},
					onKeyDown: (e) => {
						e.key === "Enter" && (e.target instanceof HTMLInputElement || e.target instanceof HTMLSelectElement) && !e.nativeEvent.isComposing && !u.current && e.preventDefault();
					},
					children: [
						/* @__PURE__ */ (0, D.jsx)("legend", { children: "New destination" }),
						/* @__PURE__ */ (0, D.jsxs)("label", { children: ["Absolute parent folder on the Snow host", /* @__PURE__ */ (0, D.jsx)("input", {
							ref: c,
							"data-op-parent": !0,
							type: "text",
							maxLength: 4096,
							placeholder: "/absolute/host/folder",
							autoComplete: "off",
							autoCapitalize: "none",
							spellCheck: !1,
							value: d.parent,
							onChange: (e) => v("parent", e.currentTarget.value)
						})] }),
						/* @__PURE__ */ (0, D.jsxs)("div", {
							className: "project-operation-actions",
							children: [/* @__PURE__ */ (0, D.jsx)("button", {
								ref: s,
								type: "button",
								className: "button",
								"data-op-browse": !0,
								onClick: () => void n.browse(d.parent),
								children: "Browse host folders"
							}), /* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "button",
								"data-op-select": !0,
								onClick: () => {
									u.current || n.select();
								},
								children: "Select parent"
							})]
						}),
						/* @__PURE__ */ (0, D.jsxs)("section", {
							className: "project-operation-folders",
							"data-op-folders": !0,
							hidden: !r.foldersOpen,
							"aria-label": "Browse host parent folders",
							children: [
								/* @__PURE__ */ (0, D.jsx)("p", {
									className: "fine",
									children: "Browsing only reads directories. Choosing a path fills the draft; select it explicitly to grant creation in that parent."
								}),
								/* @__PURE__ */ (0, D.jsxs)("div", {
									className: "project-operation-actions",
									children: [
										/* @__PURE__ */ (0, D.jsx)("button", {
											type: "button",
											className: "quiet",
											"data-op-home": !0,
											onClick: () => void n.browse(),
											children: "Home"
										}),
										/* @__PURE__ */ (0, D.jsx)("button", {
											type: "button",
											className: "quiet",
											"data-op-up": !0,
											disabled: !m || m.path === m.parent,
											onClick: () => {
												m && n.browse(m.parent);
											},
											children: "Up"
										}),
										/* @__PURE__ */ (0, D.jsx)("button", {
											type: "button",
											className: "quiet",
											"data-op-folder-close": !0,
											onClick: () => {
												n.closeFolders(), s.current?.focus();
											},
											children: "Close folder list"
										})
									]
								}),
								/* @__PURE__ */ (0, D.jsx)("p", {
									className: "mono",
									"data-op-folder-path": !0,
									children: m?.path
								}),
								/* @__PURE__ */ (0, D.jsx)("ul", {
									"data-op-folder-list": !0,
									"aria-label": "Host folders",
									children: m?.folders.map((e) => /* @__PURE__ */ (0, D.jsx)("li", { children: /* @__PURE__ */ (0, D.jsx)("button", {
										type: "button",
										className: "quiet",
										onClick: () => void n.browse(e.path),
										children: e.name
									}) }, e.path))
								}),
								/* @__PURE__ */ (0, D.jsxs)("div", {
									className: "project-operation-actions",
									children: [/* @__PURE__ */ (0, D.jsx)("button", {
										type: "button",
										className: "button",
										"data-op-folder-more": !0,
										hidden: !m?.has_more,
										onClick: () => {
											m && n.browse(m.path, m.next_offset);
										},
										children: "Next folder batch"
									}), /* @__PURE__ */ (0, D.jsx)("button", {
										type: "button",
										className: "button",
										"data-op-folder-use": !0,
										disabled: !m || r.reading,
										onClick: () => {
											m && (n.setParentPath(m.path), c.current?.focus());
										},
										children: "Use this path in draft"
									})]
								})
							]
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							"data-op-grant": !0,
							role: "status",
							children: f ? `Selected ${f.path}. Expires ${new Date(f.expires_at).toLocaleTimeString()}. One request only; host-user OS authority.` : "No parent selected. A selection lasts five minutes and authorizes only one request."
						}),
						/* @__PURE__ */ (0, D.jsxs)("div", {
							className: "project-operation-fields",
							children: [/* @__PURE__ */ (0, D.jsxs)("label", { children: ["Operation", /* @__PURE__ */ (0, D.jsxs)("select", {
								"data-op-kind": !0,
								value: d.kind,
								onChange: (e) => v("kind", e.currentTarget.value),
								children: [/* @__PURE__ */ (0, D.jsx)("option", {
									value: "create",
									children: "Create empty folder"
								}), /* @__PURE__ */ (0, D.jsx)("option", {
									value: "clone",
									children: "Clone anonymous HTTPS repository"
								})]
							})] }), /* @__PURE__ */ (0, D.jsxs)("label", { children: ["New folder name", /* @__PURE__ */ (0, D.jsx)("input", {
								"data-op-name": !0,
								type: "text",
								maxLength: 128,
								autoComplete: "off",
								autoCapitalize: "none",
								spellCheck: !1,
								placeholder: "new-project",
								value: d.name,
								onChange: (e) => v("name", e.currentTarget.value)
							})] })]
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "One name only, 1–128 UTF-8 bytes. No slashes, backslashes, control characters, surrounding whitespace, “.” or “..”. The destination must not already exist."
						}),
						/* @__PURE__ */ (0, D.jsxs)("label", {
							"data-op-remote-label": !0,
							hidden: d.kind !== "clone",
							children: ["Anonymous HTTPS repository URL", /* @__PURE__ */ (0, D.jsx)("input", {
								"data-op-remote": !0,
								type: "text",
								maxLength: 512,
								autoComplete: "off",
								autoCapitalize: "none",
								spellCheck: !1,
								placeholder: "https://example.com/team/repository.git",
								"aria-describedby": "project-operation-remote-help",
								value: d.remote,
								onChange: (e) => v("remote", e.currentTarget.value)
							})]
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							id: "project-operation-remote-help",
							className: "fine",
							"data-op-remote-help": !0,
							hidden: d.kind !== "clone",
							children: "No username, password, token query, fragment, SSH or local path. Rejected URLs are not retained as drafts."
						}),
						/* @__PURE__ */ (0, D.jsx)("button", {
							type: "button",
							className: "button primary",
							"data-op-review": !0,
							disabled: !f || Date.now() >= f.expires_at,
							onClick: () => {
								u.current || n.reviewCreation();
							},
							children: "Review destination & effects…"
						})
					]
				}),
				/* @__PURE__ */ (0, D.jsxs)("section", {
					className: "project-operation-review",
					"data-op-confirmation": !0,
					hidden: !p,
					"aria-label": "Confirm project operation",
					children: [
						/* @__PURE__ */ (0, D.jsx)("h3", {
							"data-op-confirm-title": !0,
							children: p?.title || "Review operation"
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "mono",
							"data-op-confirm-detail": !0,
							children: p?.detail
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							"data-op-confirm-effects": !0,
							children: p?.effects
						}),
						/* @__PURE__ */ (0, D.jsxs)("label", {
							className: "project-operation-checkbox",
							children: [
								/* @__PURE__ */ (0, D.jsx)("input", {
									type: "checkbox",
									"data-op-confirm-check": !0,
									checked: r.checked,
									disabled: g,
									onChange: (e) => n.checkConsent(e.currentTarget.checked)
								}),
								" ",
								"I reviewed this exact destination and the stated effects."
							]
						}),
						/* @__PURE__ */ (0, D.jsxs)("div", {
							className: "project-operation-actions",
							children: [/* @__PURE__ */ (0, D.jsx)("button", {
								ref: a,
								type: "button",
								className: "quiet",
								"data-op-back": !0,
								disabled: r.pending,
								onClick: () => {
									n.back(), o.current?.focus();
								},
								children: "Back without changing anything"
							}), /* @__PURE__ */ (0, D.jsx)("button", {
								type: "button",
								className: "primary",
								"data-op-confirm": !0,
								disabled: g || !p || !r.checked,
								onClick: () => {
									u.current || n.confirm();
								},
								children: p?.button || "Confirm operation"
							})]
						})
					]
				}),
				/* @__PURE__ */ (0, D.jsx)("p", {
					className: "project-operation-status",
					"data-op-status": !0,
					role: "status",
					"aria-live": "polite",
					children: r.status
				}),
				/* @__PURE__ */ (0, D.jsxs)("div", {
					className: "project-operation-uncertain",
					"data-op-uncertain": !0,
					hidden: !r.uncertain,
					children: [/* @__PURE__ */ (0, D.jsx)("p", {
						"data-op-uncertain-text": !0,
						children: r.uncertain && `Request ${r.uncertain}. Its response was not reviewed here. Check the durable record; do not recreate or retry a clone to discover its outcome.`
					}), /* @__PURE__ */ (0, D.jsx)("button", {
						type: "button",
						className: "button",
						"data-op-check": !0,
						disabled: g,
						onClick: () => void n.check(),
						children: "Check recorded request (read only)"
					})]
				}),
				/* @__PURE__ */ (0, D.jsxs)("section", {
					className: "project-operation-inventory",
					"aria-label": "Durable project operations",
					children: [
						/* @__PURE__ */ (0, D.jsxs)("div", {
							className: "section-heading",
							children: [/* @__PURE__ */ (0, D.jsx)("h3", { children: "Operations" }), /* @__PURE__ */ (0, D.jsx)("button", {
								ref: o,
								type: "button",
								className: "button",
								"data-op-refresh": !0,
								disabled: g,
								onClick: () => void n.refresh(),
								children: "Refresh operations"
							})]
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "One operation runs at a time; there is no queue. Up to 128 retained records. Pages contain at most 32 records and 64 KiB. Partial destinations are retained; dismissing a record never deletes files."
						}),
						/* @__PURE__ */ (0, D.jsx)("ul", {
							"data-op-list": !0,
							className: "project-operation-list",
							children: r.operations.map((e) => {
								let t = (t, r) => /* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "button",
									"data-op-row-action": r,
									disabled: _,
									onClick: () => n.reviewAction(e, r),
									children: t
								});
								return /* @__PURE__ */ (0, D.jsxs)("li", {
									"data-operation-id": e.id,
									children: [
										/* @__PURE__ */ (0, D.jsxs)("strong", { children: [
											e.name,
											" · ",
											xu[e.state]
										] }),
										/* @__PURE__ */ (0, D.jsxs)("small", { children: [
											e.kind === "clone" ? "Clone" : "Create",
											" · outcome",
											" ",
											e.outcome,
											" · revision ",
											e.revision
										] }),
										/* @__PURE__ */ (0, D.jsxs)("small", {
											className: "mono",
											children: ["Reference ", e.id]
										}),
										/* @__PURE__ */ (0, D.jsx)("small", {
											className: "mono",
											children: e.child?.path || Cu(e.parent.path, e.name)
										}),
										e.remote && /* @__PURE__ */ (0, D.jsx)("small", {
											className: "mono",
											children: e.remote
										}),
										e.project_id && /* @__PURE__ */ (0, D.jsxs)("small", {
											className: "mono",
											children: [
												"Registered project ",
												e.project_id,
												". No agent was activated."
											]
										}),
										e.outcome === "unknown" && /* @__PURE__ */ (0, D.jsx)("p", {
											className: "fine",
											children: "Ownership or completion is unknown. Observe the destination before deciding what to register; never retry the clone as recovery."
										}),
										/* @__PURE__ */ (0, D.jsx)("div", {
											className: "project-operation-actions",
											children: Su.has(e.state) ? e.state !== "cancel_requested" && t("Request stop…", "cancel") : /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
												t("Observe identity…", "reconcile"),
												!e.project_id && t(e.state === "awaiting_registration" && e.outcome === "observed" ? "Register retained destination…" : "Review ordinary registration…", "register"),
												t("Dismiss record only…", "dismiss")
											] })
										})
									]
								}, e.id);
							})
						}),
						/* @__PURE__ */ (0, D.jsxs)("div", {
							className: "project-operation-actions",
							children: [
								/* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "quiet",
									"data-op-first": !0,
									hidden: r.offset === 0,
									disabled: g,
									onClick: () => void n.refresh(),
									children: "First page"
								}),
								/* @__PURE__ */ (0, D.jsx)("button", {
									type: "button",
									className: "button",
									"data-op-next": !0,
									hidden: !r.more,
									disabled: g,
									onClick: () => void n.refresh(r.next),
									children: "Next page"
								}),
								/* @__PURE__ */ (0, D.jsx)("span", {
									className: "fine",
									"data-op-page": !0,
									children: r.operations.length ? `Records ${r.offset + 1}–${r.offset + r.operations.length}.` : "No records on this page."
								})
							]
						})
					]
				})
			]
		})]
	});
}
//#endregion
//#region src/workspace/Catalog.tsx
function Ru() {
	let { folder: e } = du();
	return /* @__PURE__ */ (0, D.jsxs)("dialog", {
		id: "folder-picker",
		className: "folder-dialog",
		"aria-labelledby": "folder-title",
		"aria-describedby": "folder-description",
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-heading",
				children: [/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("span", {
					className: "eyebrow",
					children: "ON THE SNOW HOST"
				}), /* @__PURE__ */ (0, D.jsx)("h2", {
					id: "folder-title",
					children: "Choose a workspace folder"
				})] }), /* @__PURE__ */ (0, D.jsx)("button", {
					className: "quiet",
					type: "button",
					"data-folder-close": !0,
					"aria-label": "Close folder browser",
					children: /* @__PURE__ */ (0, D.jsx)(mu, { kind: "close" })
				})]
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "folder-content",
				children: [
					/* @__PURE__ */ (0, D.jsx)("p", {
						id: "folder-description",
						className: "fine",
						children: "Browse directories on the machine running Snow. Selecting a folder fills the workspace path; it does not add the workspace yet."
					}),
					/* @__PURE__ */ (0, D.jsxs)("div", {
						className: "folder-toolbar",
						children: [
							/* @__PURE__ */ (0, D.jsx)("button", {
								className: "button",
								type: "button",
								"data-folder-home": !0,
								disabled: e.busy,
								children: "Home"
							}),
							/* @__PURE__ */ (0, D.jsx)("button", {
								className: "button",
								type: "button",
								"data-folder-up": !0,
								disabled: e.busy || !e.path || e.path === e.parent,
								children: "↑ Up"
							}),
							/* @__PURE__ */ (0, D.jsx)("span", {
								className: "fine",
								children: "Directories only"
							})
						]
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						id: "folder-current",
						className: "mono catalog-path",
						"aria-label": "Current host folder",
						children: e.path || "Loading host home…"
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						id: "folder-error",
						className: "error",
						role: "alert",
						hidden: !e.error,
						children: e.error
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						id: "folder-status",
						className: "fine",
						role: "status",
						"aria-live": "polite",
						children: e.status
					}),
					/* @__PURE__ */ (0, D.jsx)("ul", {
						id: "folder-list",
						className: "folder-list",
						"aria-label": "Host folders",
						"aria-busy": e.busy,
						children: e.folders.map((t) => /* @__PURE__ */ (0, D.jsx)("li", { children: /* @__PURE__ */ (0, D.jsx)("button", {
							type: "button",
							"data-folder-path": t.path,
							"aria-label": `Open folder ${t.name}`,
							disabled: e.busy,
							children: t.name
						}) }, t.path))
					}),
					/* @__PURE__ */ (0, D.jsx)("button", {
						className: "button folder-more",
						type: "button",
						"data-folder-more": !0,
						hidden: !e.hasMore,
						disabled: e.busy,
						children: "Load more folders"
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "dialog-actions",
				children: [/* @__PURE__ */ (0, D.jsx)("button", {
					className: "quiet",
					type: "button",
					"data-folder-close": !0,
					children: "Cancel"
				}), /* @__PURE__ */ (0, D.jsx)("button", {
					className: "primary",
					type: "button",
					"data-folder-select": !0,
					disabled: e.busy || !e.canSelect,
					children: "Select this folder"
				})]
			})
		]
	});
}
function zu({ projects: e, error: t, csrf: n, registryEnabled: r, projectOperationsEnabled: i }) {
	fu();
	let { projectPath: a } = du();
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsxs)("header", {
			className: "workspace-heading",
			children: [/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("span", {
				className: "eyebrow",
				children: "YOUR WORKSPACE"
			}), /* @__PURE__ */ (0, D.jsx)("h1", { children: "Workspaces" })] }), /* @__PURE__ */ (0, D.jsxs)("span", {
				className: "host-label",
				children: [/* @__PURE__ */ (0, D.jsx)("span", { className: "status-dot" }), "Folders on this host"]
			})]
		}),
		t && /* @__PURE__ */ (0, D.jsx)("p", {
			className: "error workspace-error",
			role: "alert",
			children: t
		}),
		r ? /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsxs)("div", {
			className: "projects-content",
			children: [
				e.length ? /* @__PURE__ */ (0, D.jsxs)("section", {
					"aria-labelledby": "registered-heading",
					children: [/* @__PURE__ */ (0, D.jsxs)("div", {
						className: "section-heading",
						children: [/* @__PURE__ */ (0, D.jsx)("h2", {
							id: "registered-heading",
							children: "Pick up where you left off"
						}), /* @__PURE__ */ (0, D.jsxs)("span", {
							className: "fine",
							children: [e.length, " registered"]
						})]
					}), /* @__PURE__ */ (0, D.jsx)("div", {
						className: "catalog-list project-list",
						children: e.map((e) => /* @__PURE__ */ (0, D.jsxs)("a", {
							className: "catalog-row",
							href: tu(e.id),
							"data-snow-navigation": "",
							children: [
								/* @__PURE__ */ (0, D.jsx)("span", {
									className: "row-icon",
									"aria-hidden": "true",
									children: /* @__PURE__ */ (0, D.jsx)(mu, { kind: "folder" })
								}),
								/* @__PURE__ */ (0, D.jsxs)("span", { children: [
									/* @__PURE__ */ (0, D.jsx)("strong", { children: e.name }),
									/* @__PURE__ */ (0, D.jsx)("span", {
										className: "mono catalog-path fine",
										children: e.path
									}),
									!e.available && /* @__PURE__ */ (0, D.jsx)("span", {
										className: "unavailable",
										children: "Folder missing or identity changed"
									})
								] }),
								/* @__PURE__ */ (0, D.jsx)("span", {
									"aria-hidden": "true",
									children: "→"
								})
							]
						}, e.id))
					})]
				}) : /* @__PURE__ */ (0, D.jsxs)("div", {
					className: "empty-state project-empty",
					children: [
						/* @__PURE__ */ (0, D.jsx)("span", {
							className: "empty-mark",
							"aria-hidden": "true",
							children: /* @__PURE__ */ (0, D.jsx)(mu, { kind: "snow" })
						}),
						/* @__PURE__ */ (0, D.jsx)("span", {
							className: "eyebrow",
							children: "START WITH A WORKSPACE"
						}),
						/* @__PURE__ */ (0, D.jsx)("h2", { children: "Good work starts here." }),
						/* @__PURE__ */ (0, D.jsxs)("p", { children: [
							"Connect a folder on the machine running Snow.",
							/* @__PURE__ */ (0, D.jsx)("br", {}),
							"Your workspaces and sessions, one focused workspace."
						] })
					]
				}),
				/* @__PURE__ */ (0, D.jsxs)("section", {
					id: "add-project",
					className: "add-project-panel",
					"aria-labelledby": "add-project-heading",
					children: [
						/* @__PURE__ */ (0, D.jsx)(gu, {}),
						/* @__PURE__ */ (0, D.jsxs)("div", { children: [/* @__PURE__ */ (0, D.jsx)("h2", {
							id: "add-project-heading",
							children: "Add a workspace"
						}), /* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "Choose an existing host folder. Registration creates a manager entry, not a new directory. Your files stay where they are."
						})] }),
						/* @__PURE__ */ (0, D.jsxs)("form", {
							method: "post",
							action: "/projects/add",
							id: "add-project-form",
							children: [
								/* @__PURE__ */ (0, D.jsx)("input", {
									type: "hidden",
									name: "csrf",
									value: n
								}),
								/* @__PURE__ */ (0, D.jsx)("label", {
									htmlFor: "project-path",
									children: "Folder on the Snow host"
								}),
								/* @__PURE__ */ (0, D.jsxs)("div", {
									className: "path-input",
									children: [/* @__PURE__ */ (0, D.jsx)("input", {
										id: "project-path",
										name: "path",
										maxLength: 4096,
										required: !0,
										autoComplete: "off",
										spellCheck: !1,
										placeholder: "/path/to/project",
										"aria-describedby": "path-help",
										value: a,
										onChange: (e) => pu.editProjectPath(e.currentTarget.value)
									}), /* @__PURE__ */ (0, D.jsx)("button", {
										className: "button",
										type: "button",
										"data-folder-open": !0,
										children: "Browse folders"
									})]
								}),
								/* @__PURE__ */ (0, D.jsx)("p", {
									id: "path-help",
									className: "fine",
									children: "Browse the host filesystem, or enter an absolute path manually. This is not a browser upload."
								}),
								/* @__PURE__ */ (0, D.jsxs)("label", {
									htmlFor: "project-name",
									children: ["Display name ", /* @__PURE__ */ (0, D.jsx)("span", {
										className: "optional",
										children: "optional"
									})]
								}),
								/* @__PURE__ */ (0, D.jsx)("input", {
									id: "project-name",
									name: "name",
									maxLength: 128,
									autoComplete: "off",
									placeholder: "Defaults to the folder name"
								}),
								/* @__PURE__ */ (0, D.jsxs)("div", {
									className: "form-footer",
									children: [/* @__PURE__ */ (0, D.jsx)("span", {
										className: "fine",
										children: "No files created or modified"
									}), /* @__PURE__ */ (0, D.jsxs)("button", {
										className: "primary",
										type: "submit",
										children: ["Add workspace ", /* @__PURE__ */ (0, D.jsx)("span", {
											"aria-hidden": "true",
											children: "→"
										})]
									})]
								})
							]
						})
					]
				}),
				/* @__PURE__ */ (0, D.jsx)(Iu, {
					csrf: n,
					enabled: i
				})
			]
		}), /* @__PURE__ */ (0, D.jsx)(Ru, {})] }) : /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "empty-state",
			children: [
				/* @__PURE__ */ (0, D.jsx)("span", {
					className: "empty-mark",
					"aria-hidden": "true",
					children: "▱"
				}),
				/* @__PURE__ */ (0, D.jsx)("h2", { children: "Workspace registry unavailable" }),
				/* @__PURE__ */ (0, D.jsxs)("p", { children: [
					"Start the installed Snow binary with ",
					/* @__PURE__ */ (0, D.jsx)("code", { children: "snow --mode web" }),
					" to enable persistent workspace registration."
				] })
			]
		})
	] });
}
//#endregion
//#region src/workspace/Cold.tsx
function Bu({ project: e, sessionID: t, sessionTitle: n, error: r }) {
	let { flowError: i, inspectorOpen: a } = du();
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		/* @__PURE__ */ (0, D.jsxs)("header", {
			className: "workspace-heading project-chat-heading",
			children: [/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "project-heading",
				children: [/* @__PURE__ */ (0, D.jsx)("h1", { children: t ? n || "Untitled session" : r ? "Workspace" : "New session" }), /* @__PURE__ */ (0, D.jsx)("span", {
					className: "fine",
					children: e.name
				})]
			}), /* @__PURE__ */ (0, D.jsx)("button", {
				className: "quiet icon-button",
				type: "button",
				"data-inspector-toggle": !0,
				"aria-controls": "project-inspector",
				"aria-expanded": a,
				"aria-label": "Files and changes",
				title: "Files and changes",
				children: /* @__PURE__ */ (0, D.jsx)(mu, { kind: "panel" })
			})]
		}),
		r && /* @__PURE__ */ (0, D.jsx)("p", {
			className: "error workspace-error",
			role: "alert",
			children: r
		}),
		i && /* @__PURE__ */ (0, D.jsx)("p", {
			className: "error",
			id: "workspace-flow-error",
			role: "alert",
			children: i
		})
	] });
}
function Vu({ project: e, csrf: t, sessionID: n }) {
	let { draft: r, activation: i } = du();
	return /* @__PURE__ */ (0, D.jsxs)("section", {
		className: `runtime-activation activation-panel workspace-session-start${e.trusted ? " activation-trusted" : ""}`,
		"aria-labelledby": "activation-heading",
		children: [
			/* @__PURE__ */ (0, D.jsx)(gu, {}),
			/* @__PURE__ */ (0, D.jsx)("h2", {
				id: "activation-heading",
				children: n ? "Paused · resume to continue" : "Start a session"
			}),
			/* @__PURE__ */ (0, D.jsxs)("form", {
				method: "post",
				action: `/projects/${e.id}/runtime/open`,
				"data-runtime-open": !0,
				children: [
					/* @__PURE__ */ (0, D.jsx)("input", {
						type: "hidden",
						name: "csrf",
						value: t
					}),
					n && /* @__PURE__ */ (0, D.jsx)("input", {
						type: "hidden",
						name: "session_id",
						value: n
					}),
					/* @__PURE__ */ (0, D.jsx)("label", {
						className: "visually-hidden",
						htmlFor: "workspace-prompt",
						children: "Session draft"
					}),
					/* @__PURE__ */ (0, D.jsx)("textarea", {
						id: "workspace-prompt",
						className: "activation-draft",
						rows: 2,
						maxLength: 65536,
						"data-draft-project": e.id,
						"data-draft-session": n,
						"data-draft-name": e.name,
						placeholder: "What would you like to work on?",
						"aria-describedby": "workspace-draft-help",
						disabled: !r.workspaceEnabled || i.busy,
						value: r.workspaceText,
						onChange: (e) => pu.editDraft(e.currentTarget.value, !0)
					}),
					/* @__PURE__ */ (0, D.jsxs)("p", {
						className: "fine",
						id: "workspace-draft-help",
						children: [
							"Drafts stay in this tab. ",
							n ? "Resume" : "Start",
							", then review and send."
						]
					}),
					e.trusted ? /* @__PURE__ */ (0, D.jsx)("input", {
						type: "hidden",
						name: "confirm",
						value: "trusted"
					}) : /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "activation-boundary",
							children: "Starting loads this workspace’s configuration and instructions. Tools run with your host account’s privileges, not in a sandbox. Browsing does not start an agent."
						}),
						/* @__PURE__ */ (0, D.jsx)("input", {
							type: "hidden",
							name: "remember_trust",
							value: "project"
						}),
						/* @__PURE__ */ (0, D.jsxs)("label", {
							className: "checkbox-label",
							children: [
								/* @__PURE__ */ (0, D.jsx)("input", {
									name: "confirm",
									type: "checkbox",
									value: "activate",
									disabled: i.busy,
									required: !0
								}),
								" ",
								"I trust this workspace. Remember my choice for future starts in this manager."
							]
						}),
						/* @__PURE__ */ (0, D.jsx)("p", {
							className: "fine",
							children: "Trust does not grant tools Allow permissions. Forget it in Settings → Workspaces."
						})
					] }),
					/* @__PURE__ */ (0, D.jsxs)("details", {
						className: "activation-model-help",
						children: [
							/* @__PURE__ */ (0, D.jsx)("summary", { children: "Startup settings" }),
							n && /* @__PURE__ */ (0, D.jsx)("p", {
								className: "fine",
								"data-saved-policy-note": !0,
								children: "Resuming restores saved session permissions. A saved Allow policy skips tool approval prompts."
							}),
							/* @__PURE__ */ (0, D.jsxs)("p", {
								className: "fine",
								"data-skills-startup-policy": !0,
								children: [
									"Installed skills are ",
									e.skillsEnabled ? "enabled" : "disabled",
									" ",
									"for this workspace. Change future starts in",
									" ",
									/* @__PURE__ */ (0, D.jsx)("button", {
										type: "button",
										className: "quiet",
										"data-settings-open": "workspaces",
										onClick: (e) => {
											e.stopPropagation(), U.openSettings("workspaces", e.currentTarget);
										},
										children: "Settings → Workspaces"
									}),
									". Project skills still require separate CLI extension trust."
								]
							}),
							/* @__PURE__ */ (0, D.jsx)("p", {
								className: "fine",
								children: "Uses the host’s configured provider and model. Change models while idle after starting. If the default cannot start, configure it on the host first."
							}),
							e.trusted && /* @__PURE__ */ (0, D.jsxs)("p", {
								className: "fine",
								children: [
									"Workspace trust remembered. Manage it in",
									" ",
									/* @__PURE__ */ (0, D.jsx)("button", {
										type: "button",
										className: "quiet",
										"data-settings-open": "workspaces",
										onClick: (e) => {
											e.stopPropagation(), U.openSettings("workspaces", e.currentTarget);
										},
										children: "Settings → Workspaces"
									}),
									"."
								]
							})
						]
					}),
					/* @__PURE__ */ (0, D.jsx)("p", {
						className: "error",
						"data-action-error": !0,
						role: "alert",
						hidden: !i.error,
						children: i.error
					}),
					/* @__PURE__ */ (0, D.jsxs)("div", {
						className: "form-footer",
						children: [/* @__PURE__ */ (0, D.jsx)("span", {
							className: "fine",
							children: !n && "New sessions start in Ask"
						}), /* @__PURE__ */ (0, D.jsxs)("button", {
							className: "primary",
							type: "submit",
							disabled: i.busy,
							children: [
								n ? "Resume session" : e.trusted ? "Start session" : "Trust & start",
								" ",
								/* @__PURE__ */ (0, D.jsx)("span", {
									"aria-hidden": "true",
									children: "→"
								})
							]
						})]
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsx)("noscript", { children: /* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				children: "Live controls need JavaScript. Workspace registration and saved history remain available without it."
			}) })
		]
	});
}
function Hu(e) {
	fu();
	let { project: t, sessionID: n, error: r, hasHistory: i, nextURL: a, recoveryMessage: o, recoveryURL: s, runtimeEnabled: c, history: l } = e;
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [
		o && /* @__PURE__ */ (0, D.jsxs)("aside", {
			className: "notice recovery-notice",
			"aria-label": "Saved conversation recovery",
			children: [
				/* @__PURE__ */ (0, D.jsx)("p", { children: o }),
				/* @__PURE__ */ (0, D.jsx)("p", {
					className: "fine",
					children: "Tools may already have had effects. Reads do not reactivate workers, replay prompts, or restore approvals."
				}),
				s && /* @__PURE__ */ (0, D.jsx)("a", {
					href: s,
					"data-snow-navigation": "",
					children: "Review the last opened saved conversation"
				})
			]
		}),
		i ? /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "conversation-stream",
			children: [l ?? /* @__PURE__ */ (0, D.jsx)("div", { id: "workspace-history-view" }), a && /* @__PURE__ */ (0, D.jsx)("a", {
				className: "button",
				href: a,
				"data-snow-navigation": "",
				children: "Next page →"
			})]
		}) : /* @__PURE__ */ (0, D.jsx)("div", {
			id: "live-session",
			className: "live-session project-session-list",
			"data-project": t.id,
			children: /* @__PURE__ */ (0, D.jsx)("div", {
				className: "conversation-stream",
				children: /* @__PURE__ */ (0, D.jsxs)("div", {
					className: "empty-state compact",
					children: [/* @__PURE__ */ (0, D.jsx)("h2", { children: n ? "Session history unavailable" : r ? "Sessions unavailable" : "What would you like to work on?" }), /* @__PURE__ */ (0, D.jsx)("p", { children: n ? "Retry this read, or explicitly resume below. Nothing has started automatically." : r ? "Saved sessions could not be read. Nothing has started. Starting below creates a new session; it does not resume an unread session." : `A new session in ${t.name}. Your other sessions stay in the sidebar.` })]
				})
			})
		}),
		c && t.available ? /* @__PURE__ */ (0, D.jsx)(Vu, { ...e }) : !i && /* @__PURE__ */ (0, D.jsxs)("div", {
			className: "empty-state compact",
			children: [/* @__PURE__ */ (0, D.jsx)("h2", { children: t.available ? "Live sessions unavailable" : "Workspace folder unavailable" }), /* @__PURE__ */ (0, D.jsx)("p", { children: t.available ? "You can browse saved conversations without starting an agent." : "The folder is missing or its identity has changed. Check the host folder before starting this workspace." })]
		})
	] });
}
function Uu(e) {
	return /* @__PURE__ */ (0, D.jsxs)(D.Fragment, { children: [/* @__PURE__ */ (0, D.jsx)(Bu, { ...e }), /* @__PURE__ */ (0, D.jsx)(Hu, { ...e })] });
}
//#endregion
//#region src/workspace/Login.tsx
function Wu({ csrf: e, error: t, networkProfile: n }) {
	return /* @__PURE__ */ (0, D.jsxs)("main", {
		className: "login-card",
		children: [
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "brand",
				children: [
					/* @__PURE__ */ (0, D.jsx)("span", {
						className: "brand-mark",
						"aria-hidden": "true",
						children: /* @__PURE__ */ (0, D.jsx)(mu, { kind: "snow" })
					}),
					/* @__PURE__ */ (0, D.jsx)("span", {
						className: "brand-name",
						children: "snow"
					}),
					/* @__PURE__ */ (0, D.jsx)("span", {
						className: "brand-label",
						children: "WORKSPACE"
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsx)("span", {
				className: "eyebrow",
				children: "ONE OPERATOR. ONE HOST."
			}),
			/* @__PURE__ */ (0, D.jsx)("h1", { children: "Your work, right here." }),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "lede",
				children: "Pair this browser with the pairing code from the terminal where Snow is running."
			}),
			t && /* @__PURE__ */ (0, D.jsx)("p", {
				className: "error",
				role: "alert",
				children: t
			}),
			/* @__PURE__ */ (0, D.jsxs)("form", {
				method: "post",
				action: "/login",
				children: [
					/* @__PURE__ */ (0, D.jsx)("input", {
						type: "hidden",
						name: "csrf",
						value: e
					}),
					/* @__PURE__ */ (0, D.jsx)("label", {
						htmlFor: "code",
						children: "Pairing code"
					}),
					/* @__PURE__ */ (0, D.jsx)("input", {
						id: "code",
						name: "code",
						type: "password",
						required: !0,
						maxLength: 128,
						autoComplete: "off",
						spellCheck: !1,
						autoFocus: !0
					}),
					/* @__PURE__ */ (0, D.jsxs)("button", {
						className: "primary",
						type: "submit",
						children: ["Connect to this host ", /* @__PURE__ */ (0, D.jsx)("span", {
							"aria-hidden": "true",
							children: "→"
						})]
					})
				]
			}),
			/* @__PURE__ */ (0, D.jsx)("p", {
				className: "fine",
				children: "The pairing code is reusable for up to 30 days, or until rotated. Paired browsers stay connected across Snow restarts. Credentials stay out of URLs and browser history."
			}),
			/* @__PURE__ */ (0, D.jsxs)("div", {
				className: "local-note",
				children: [/* @__PURE__ */ (0, D.jsx)("span", { className: "status-dot" }), n === "trusted-lan-http" ? "Trusted-LAN HTTP" : "Direct loopback HTTP"]
			})
		]
	});
}
//#endregion
//#region src/workspace/Opening.tsx
function Gu() {
	let { opening: e } = du();
	return e.visible ? /* @__PURE__ */ (0, D.jsx)("div", {
		id: "workspace-opening",
		className: "empty-state compact workspace-opening",
		role: "status",
		"aria-live": "polite",
		style: { minHeight: `min(${e.minHeight}px, 100dvh)` },
		children: /* @__PURE__ */ (0, D.jsx)("p", { children: "Opening selected conversation…" })
	}) : null;
}
//#endregion
//#region src/navigation.ts
var Ku = 8388608, qu = 1e4, Ju = null, Yu = {
	beforeReplace() {},
	afterReplace() {}
};
"scrollRestoration" in window.history && (window.history.scrollRestoration = "manual");
function Xu(e) {
	return e && typeof e == "object" && !Array.isArray(e) ? e : {};
}
var Zu = 256, Qu = /* @__PURE__ */ new Map();
function $u(e) {
	let t = Xu(e).snowNavigationEntry;
	return typeof t == "string" && t.length > 0 && t.length <= 64 ? t : null;
}
function ed() {
	if (typeof window.crypto.randomUUID == "function") return window.crypto.randomUUID();
	let e = /* @__PURE__ */ new Uint8Array(16);
	window.crypto.getRandomValues(e), e[6] = e[6] & 15 | 64, e[8] = e[8] & 63 | 128;
	let t = Array.from(e, (e) => e.toString(16).padStart(2, "0")).join("");
	return `${t.slice(0, 8)}-${t.slice(8, 12)}-${t.slice(12, 16)}-${t.slice(16, 20)}-${t.slice(20)}`;
}
function G() {
	return {
		x: Math.min(Math.max(0, window.scrollX), 1e7),
		y: Math.min(Math.max(0, window.scrollY), 1e7)
	};
}
function K(e, t = G()) {
	if (!Qu.has(e) && Qu.size >= Zu) {
		let e = Qu.keys().next().value;
		e && Qu.delete(e);
	}
	Qu.set(e, t);
}
var q = $u(window.history.state), J = q || ed();
q || window.history.replaceState({
	...Xu(window.history.state),
	snowNavigation: !0,
	snowNavigationEntry: J
}, "", window.location.href), K(J), window.addEventListener("scroll", () => {
	Ju || K(J);
}, { passive: !0 });
function Y(e, t) {
	document.dispatchEvent(new CustomEvent(e, { detail: t }));
}
async function td(e, t) {
	if (!e.body) throw Error("Navigation response is empty");
	let n = e.body.getReader(), r = new TextDecoder("utf-8", { fatal: !0 }), i = 0, a = "";
	try {
		for (;;) {
			let e = await n.read();
			if (e.done) break;
			if (t.throwIfAborted(), i += e.value.byteLength, i > Ku) throw Error("Navigation response is too large");
			a += r.decode(e.value, { stream: !0 });
		}
		return a + r.decode();
	} finally {
		await n.cancel(), n.releaseLock();
	}
}
function nd(e) {
	if (typeof e != "string" || !e || e.length > 8192) throw Error("Invalid navigation destination");
	let t = new URL(e, window.location.href);
	if (t.origin !== window.location.origin || t.username || t.password) throw Error("Cross-origin navigation is unavailable");
	return t;
}
function rd(e) {
	let t = new DOMParser().parseFromString(e, "text/html").querySelectorAll("#workspace");
	if (t.length !== 1) throw Error("Navigation response has no unique workspace");
	return document.importNode(t[0], !0);
}
async function id(e, t = {}) {
	let n = nd(e), r = t.method || "GET";
	if (r === "GET" && n.pathname !== "/" || r === "POST" && n.pathname !== "/projects/add") throw Error("Unsupported navigation destination");
	let i = document.getElementById("workspace");
	if (!i) throw Error("Workspace is unavailable");
	Ju || K(J), Ju?.abort();
	let a = new AbortController(), o = window.setTimeout(() => a.abort(new DOMException("Navigation timed out", "TimeoutError")), qu);
	Ju = a;
	let s = {
		target: i,
		source: t.source || null,
		requestConfig: {
			path: n.pathname,
			method: r
		}
	};
	Y("snow:navigation-start", s);
	let c = !1;
	try {
		let e = await fetch(n, {
			method: r,
			body: r === "POST" && t.body || null,
			credentials: "same-origin",
			cache: "no-store",
			redirect: "follow",
			signal: a.signal,
			headers: {
				Accept: "text/html",
				"X-Snow-Navigation": "workspace"
			}
		});
		if (a.signal.throwIfAborted(), e.status === 401) throw c = !0, window.location.assign("/login"), new DOMException("Browser pairing is required", "AbortError");
		if (!e.ok || !/^text\/html(?:;|$)/i.test(e.headers.get("Content-Type") || "")) throw Error("Navigation request failed");
		let o = nd(e.url);
		if (o.pathname !== "/") throw Error("Unexpected navigation response");
		let l = rd(await td(e, a.signal));
		if (a.signal.throwIfAborted(), Ju !== a || !i.isConnected || i !== document.getElementById("workspace")) throw new DOMException("Navigation was superseded", "AbortError");
		Yu.beforeReplace(i), Y("snow:navigation-before-swap", s), i.replaceWith(l);
		let u = t.history || "none", d = r === "GET" ? n : o, f = t.entryID || $u(window.history.state);
		u === "push" ? (f = ed(), window.history.pushState({
			snowNavigation: !0,
			snowNavigationEntry: f
		}, "", d)) : u === "replace" ? (f = ed(), window.history.replaceState({
			...Xu(window.history.state),
			snowNavigation: !0,
			snowNavigationEntry: f
		}, "", d)) : f || (f = ed(), window.history.replaceState({
			...Xu(window.history.state),
			snowNavigation: !0,
			snowNavigationEntry: f
		}, "", window.location.href)), J = f, Yu.afterReplace(l), Y("snow:navigation-after-swap", {
			...s,
			target: l
		}), t.restoreScroll === void 0 && (d.hash ? document.getElementById(d.hash.slice(1))?.scrollIntoView({ block: "start" }) : window.scrollTo({
			left: 0,
			top: 0,
			behavior: "auto"
		}), K(J));
	} catch (e) {
		let t = a.signal.aborted || e instanceof DOMException && e.name === "AbortError";
		throw Ju === a && (Y("snow:navigation-error", {
			...s,
			aborted: t
		}), !c && $u(window.history.state) !== J && window.location.reload()), e;
	} finally {
		window.clearTimeout(o), Ju === a && (Ju = null, Y("snow:navigation-end", {
			...s,
			aborted: a.signal.aborted
		}));
	}
}
var ad = Object.freeze({
	configure(e) {
		Yu = e;
	},
	visit(e, t = {}) {
		return id(e, {
			...t,
			method: "GET"
		});
	},
	submit(e, t, n = {}) {
		return id(e, {
			...n,
			method: "POST",
			body: t
		});
	},
	abort() {
		Ju?.abort();
	}
});
window.addEventListener("popstate", (e) => {
	if (!document.getElementById("workspace")) return;
	let t = $u(e.state);
	t || (t = ed(), window.history.replaceState({
		...Xu(e.state),
		snowNavigation: !0,
		snowNavigationEntry: t
	}, "", window.location.href));
	let n = Qu.get(t) || null, r = new URL(window.location.href);
	id(r.href, {
		method: "GET",
		history: "none",
		entryID: t,
		restoreScroll: n
	}).then(() => {
		Ju || J !== t || (n ? window.scrollTo({
			left: n.x,
			top: n.y,
			behavior: "auto"
		}) : r.hash ? document.getElementById(r.hash.slice(1))?.scrollIntoView({ block: "start" }) : window.scrollTo({
			left: 0,
			top: 0,
			behavior: "auto"
		}), K(t));
	}).catch((e) => {
		e instanceof DOMException && e.name === "AbortError" || window.location.reload();
	});
});
//#endregion
//#region src/main.tsx
var X = /* @__PURE__ */ new Map(), od = /* @__PURE__ */ new WeakMap(), sd = 1048576, cd = class extends l.Component {
	state = { failed: !1 };
	static getDerivedStateFromError() {
		return { failed: !0 };
	}
	componentDidCatch(e, t) {}
	render() {
		return this.state.failed ? /* @__PURE__ */ (0, D.jsx)("p", {
			className: "error",
			role: "alert",
			children: "This page could not be displayed. Reload to try again. No action has been retried."
		}) : this.props.children;
	}
};
function ld({ element: e, children: t }) {
	return (0, l.useLayoutEffect)(() => (e.dataset.reactMounted = "true", () => {
		delete e.dataset.reactMounted;
	}), [e]), /* @__PURE__ */ (0, D.jsx)(cd, { children: t });
}
function ud(e) {
	let t = e.dataset.reactProps || "{}";
	if (t.length > sd) throw Error("Page data too large");
	let n = JSON.parse(t);
	switch (e.dataset.reactPage) {
		case "home": return /* @__PURE__ */ (0, D.jsx)(hu, { ...Ql(n) });
		case "workspace-catalog": return /* @__PURE__ */ (0, D.jsx)(zu, { ...eu(n) });
		case "workspace-cold": {
			let t = ru(n), r = od.get(e);
			return (!r || r.project !== t.project.id || r.session !== t.sessionID) && (r = {
				project: t.project.id,
				session: t.sessionID,
				projection: B(e.querySelector(".catalog-history"), {
					project: t.project.id,
					session: t.sessionID
				})
			}, od.set(e, r)), /* @__PURE__ */ (0, D.jsx)(Uu, {
				...t,
				history: /* @__PURE__ */ (0, D.jsx)(Ko, { projection: r.projection })
			});
		}
		case "login": return /* @__PURE__ */ (0, D.jsx)(Wu, { ...$l(n) });
		case "startup-draft-notice": return /* @__PURE__ */ (0, D.jsx)(gu, {});
		case "workspace-opening": return /* @__PURE__ */ (0, D.jsx)(Gu, {});
		case "shell": {
			let e = document.getElementById("shell-navigation-root");
			if (!e) throw Error("Missing navigation host");
			return /* @__PURE__ */ (0, D.jsx)(Gl, {
				bootstrap: Sl(n),
				navigationHost: e,
				navigate: (e, t) => {
					document.dispatchEvent(new CustomEvent("snow:shell-navigate", { detail: {
						href: e,
						source: t
					} }));
				},
				command: (e) => document.dispatchEvent(new CustomEvent("snow:shell-command", { detail: e }))
			});
		}
		case "activity":
			if (!n || typeof n != "object" || !("registryEnabled" in n) || typeof n.registryEnabled != "boolean" || !("error" in n) || typeof n.error != "string" || n.error.length > 4096) throw Error("Invalid activity page");
			return /* @__PURE__ */ (0, D.jsx)(se, {
				registryEnabled: n.registryEnabled,
				error: n.error
			});
		case "organization": return /* @__PURE__ */ (0, D.jsx)(Ee, { ...be(n) });
		case "browser-access":
			if (!n || typeof n != "object" || !("csrf" in n) || typeof n.csrf != "string" || n.csrf.length > 512) throw Error("Invalid browser access page");
			return /* @__PURE__ */ (0, D.jsx)(Re, { csrf: n.csrf });
		case "host-settings": {
			if (!n || typeof n != "object" || !("csrf" in n) || typeof n.csrf != "string" || n.csrf.length > 512 || !("enabled" in n) || typeof n.enabled != "boolean" || !("projects" in n) || !Array.isArray(n.projects) || n.projects.length > 100) throw Error("Invalid host settings");
			let e = /* @__PURE__ */ new Set(), t = n.projects.map((t) => {
				if (!t || typeof t != "object" || !("id" in t) || typeof t.id != "string" || !/^[0-9a-f]{8}-(?:[0-9a-f]{4}-){3}[0-9a-f]{12}$/.test(t.id) || e.has(t.id) || !("name" in t) || typeof t.name != "string" || new TextEncoder().encode(t.name).length > 128) throw Error("Invalid project");
				return e.add(t.id), {
					id: t.id,
					name: t.name
				};
			});
			return /* @__PURE__ */ (0, D.jsx)(tt, {
				csrf: n.csrf,
				enabled: n.enabled,
				projects: t
			});
		}
		default: throw Error("Unsupported page");
	}
}
function dd() {
	for (let [e, t] of X) e.isConnected || (t.unmount(), X.delete(e));
	document.querySelectorAll("[data-react-page]").forEach((e) => {
		if (X.has(e) || e.parentElement?.closest("[data-react-page]")) return;
		let t;
		try {
			t = ud(e);
		} catch {
			t = /* @__PURE__ */ (0, D.jsx)("p", {
				className: "error",
				role: "alert",
				children: "This page could not be loaded safely. Reload to try again. No action has been sent."
			});
		}
		let n = (0, d.createRoot)(e, {
			onCaughtError: () => {},
			onUncaughtError: () => {},
			onRecoverableError: () => {}
		});
		X.set(e, n), (0, u.flushSync)(() => n.render(/* @__PURE__ */ (0, D.jsx)(ld, {
			element: e,
			children: t
		})));
	});
}
function fd(e) {
	for (let [t, n] of X) (e === t || e.contains(t)) && (n.unmount(), X.delete(t));
}
ad.configure({
	beforeReplace: fd,
	afterReplace: dd
}), window.addEventListener("pageshow", dd), window.addEventListener("pagehide", () => {
	for (let e of X.values()) e.unmount();
	X.clear();
}), document.readyState === "loading" ? document.addEventListener("DOMContentLoaded", dd, { once: !0 }) : dd(), Object.assign(window, {
	SnowReasoning: ht,
	SnowCompaction: It,
	SnowGoals: dn,
	SnowVersions: ir,
	SnowHistoryControls: ar,
	SnowConversation: Vr,
	SnowSteer: Ei,
	SnowQueue: Ji,
	SnowComposerContext: ga,
	SnowLiveView: Fa,
	SnowMessages: Bo,
	SnowVisibility: Vo,
	SnowInspection: Ns,
	SnowProcesses: ic,
	SnowAttention: Hc,
	SnowScroll: tl,
	SnowWidth: hl,
	SnowShell: U,
	SnowSidebarSessions: Ol,
	SnowSessionActions: kl,
	SnowWorkspace: pu,
	SnowProjectOperations: Fu,
	SnowNavigation: ad,
	SnowReactReady: !0
}), document.dispatchEvent(new Event("snow:react-ready"));
//#endregion
