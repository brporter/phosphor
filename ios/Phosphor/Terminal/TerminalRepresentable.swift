import SwiftUI
import SwiftTerm

#if os(iOS) || targetEnvironment(macCatalyst)

struct TerminalRepresentable: UIViewRepresentable {
    let viewModel: TerminalViewModel

    func makeCoordinator() -> Coordinator {
        Coordinator(viewModel: viewModel)
    }

    func makeUIView(context: Context) -> SwiftTerm.TerminalView {
        let termView = SwiftTerm.TerminalView(frame: .zero)
        termView.terminalDelegate = context.coordinator
        context.coordinator.terminalView = termView

        // Configure appearance
        termView.nativeBackgroundColor = UIColor(red: 0.04, green: 0.04, blue: 0.04, alpha: 1) // #0A0A0A
        termView.nativeForegroundColor = UIColor(red: 0.69, green: 0.69, blue: 0.69, alpha: 1) // #B0B0B0

        let fontSize: CGFloat = UIDevice.current.userInterfaceIdiom == .pad ? 14 : 12
        termView.font = UIFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)

        // Wire stdout from WebSocket -> terminal
        viewModel.onStdout = { data in
            let bytes = ArraySlice([UInt8](data))
            DispatchQueue.main.async {
                termView.feed(byteArray: bytes)
            }
        }

        // Wire resize events
        viewModel.onResize = { cols, rows in
            DispatchQueue.main.async {
                termView.resize(cols: cols, rows: rows)
            }
        }

        return termView
    }

    func updateUIView(_ uiView: SwiftTerm.TerminalView, context: Context) {
        // Send initial terminal size and grab focus once the view has been laid out
        if !context.coordinator.hasReportedInitialSize && uiView.bounds.width > 0 && uiView.bounds.height > 0 {
            let terminal = uiView.getTerminal()
            if terminal.cols > 0 && terminal.rows > 0 {
                context.coordinator.hasReportedInitialSize = true
                viewModel.sendResize(cols: terminal.cols, rows: terminal.rows)
                uiView.becomeFirstResponder()
            }
        }
    }

    class Coordinator: NSObject, SwiftTerm.TerminalViewDelegate {
        let viewModel: TerminalViewModel
        weak var terminalView: SwiftTerm.TerminalView?
        var hasReportedInitialSize = false

        init(viewModel: TerminalViewModel) {
            self.viewModel = viewModel
        }

        func send(source: SwiftTerm.TerminalView, data: ArraySlice<UInt8>) {
            let bytes = Data(data)
            viewModel.sendStdin(bytes)
        }

        func scrolled(source: SwiftTerm.TerminalView, position: Double) {}

        func setTerminalTitle(source: SwiftTerm.TerminalView, title: String) {}

        func hostCurrentDirectoryUpdate(source: SwiftTerm.TerminalView, directory: String?) {}

        func sizeChanged(source: SwiftTerm.TerminalView, newCols: Int, newRows: Int) {
            viewModel.sendResize(cols: newCols, rows: newRows)
        }

        func requestOpenLink(source: SwiftTerm.TerminalView, link: String, params: [String: String]) {
            if let url = URL(string: link) {
                UIApplication.shared.open(url)
            }
        }

        func bell(source: SwiftTerm.TerminalView) {}

        func clipboardCopy(source: SwiftTerm.TerminalView, content: Data) {
            if let str = String(data: content, encoding: .utf8) {
                UIPasteboard.general.string = str
            }
        }

        func iTermContent(source: SwiftTerm.TerminalView, content: ArraySlice<UInt8>) {}

        func rangeChanged(source: SwiftTerm.TerminalView, startY: Int, endY: Int) {}
    }
}

#elseif os(macOS)

struct TerminalRepresentable: NSViewRepresentable {
    let viewModel: TerminalViewModel

    func makeCoordinator() -> Coordinator {
        Coordinator(viewModel: viewModel)
    }

    func makeNSView(context: Context) -> SwiftTerm.TerminalView {
        let termView = SwiftTerm.TerminalView(frame: .zero)
        termView.terminalDelegate = context.coordinator
        context.coordinator.terminalView = termView

        // Configure appearance
        termView.nativeBackgroundColor = NSColor(red: 0.04, green: 0.04, blue: 0.04, alpha: 1)
        termView.nativeForegroundColor = NSColor(red: 0.69, green: 0.69, blue: 0.69, alpha: 1)

        let fontSize: CGFloat = 14
        if let firaCode = NSFont(name: "Fira Code", size: fontSize) {
            termView.font = firaCode
        } else {
            termView.font = NSFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
        }

        // Wire stdout from WebSocket -> terminal
        viewModel.onStdout = { data in
            let bytes = ArraySlice([UInt8](data))
            DispatchQueue.main.async {
                termView.feed(byteArray: bytes)
            }
        }

        // Wire resize events
        viewModel.onResize = { cols, rows in
            DispatchQueue.main.async {
                termView.resize(cols: cols, rows: rows)
            }
        }

        return termView
    }

    func updateNSView(_ nsView: SwiftTerm.TerminalView, context: Context) {
        // Send initial terminal size and grab focus once the view has been laid out
        if !context.coordinator.hasReportedInitialSize && nsView.bounds.width > 0 && nsView.bounds.height > 0 {
            let terminal = nsView.getTerminal()
            if terminal.cols > 0 && terminal.rows > 0 {
                context.coordinator.hasReportedInitialSize = true
                viewModel.sendResize(cols: terminal.cols, rows: terminal.rows)
                nsView.window?.makeFirstResponder(nsView)
            }
        }
    }

    class Coordinator: NSObject, SwiftTerm.TerminalViewDelegate {
        let viewModel: TerminalViewModel
        weak var terminalView: SwiftTerm.TerminalView?
        var hasReportedInitialSize = false

        init(viewModel: TerminalViewModel) {
            self.viewModel = viewModel
        }

        func send(source: SwiftTerm.TerminalView, data: ArraySlice<UInt8>) {
            let bytes = Data(data)
            viewModel.sendStdin(bytes)
        }

        func scrolled(source: SwiftTerm.TerminalView, position: Double) {}

        func setTerminalTitle(source: SwiftTerm.TerminalView, title: String) {}

        func hostCurrentDirectoryUpdate(source: SwiftTerm.TerminalView, directory: String?) {}

        func sizeChanged(source: SwiftTerm.TerminalView, newCols: Int, newRows: Int) {
            viewModel.sendResize(cols: newCols, rows: newRows)
        }

        func requestOpenLink(source: SwiftTerm.TerminalView, link: String, params: [String: String]) {
            if let url = URL(string: link) {
                NSWorkspace.shared.open(url)
            }
        }

        func bell(source: SwiftTerm.TerminalView) {}

        func clipboardCopy(source: SwiftTerm.TerminalView, content: Data) {
            if let str = String(data: content, encoding: .utf8) {
                NSPasteboard.general.clearContents()
                NSPasteboard.general.setString(str, forType: .string)
            }
        }

        func iTermContent(source: SwiftTerm.TerminalView, content: ArraySlice<UInt8>) {}

        func rangeChanged(source: SwiftTerm.TerminalView, startY: Int, endY: Int) {}
    }
}

#endif
