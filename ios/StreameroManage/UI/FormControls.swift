import SwiftUI

/// A number edited as text; keeps what the user is typing ("1.") and writes a JSON number.
struct JSONNumberField: View {
    @Binding var value: JSONValue
    var isFloat = false
    @State private var text = ""

    var body: some View {
        TextField(text: $text, prompt: Text(verbatim: "0")) { EmptyView() }
            .keyboardType(isFloat ? .decimalPad : .numbersAndPunctuation)
            .multilineTextAlignment(.trailing)
            .frame(minWidth: 70)
            .onAppear { text = value.text }
            .onChange(of: value) { _, now in
                if Self.parse(text, isFloat: isFloat) != now { text = now.text }
            }
            .onChange(of: text) { _, now in
                let parsed = Self.parse(now, isFloat: isFloat)
                if parsed != value, parsed != nil || now.isEmpty { value = parsed ?? .null }
            }
    }

    static func parse(_ text: String, isFloat: Bool) -> JSONValue? {
        let t = text.trimmingCharacters(in: .whitespaces).replacingOccurrences(of: ",", with: ".")
        if t.isEmpty { return nil }
        if !isFloat, let i = Int64(t) { return .int(i) }
        if let d = Double(t) { return isFloat ? .double(d) : (d.rounded() == d ? .int(Int64(d)) : .double(d)) }
        return nil
    }
}

/// Picks several values from a list (multi-select fields, tags).
struct MultiSelectList: View {
    let title: String
    let options: [ArrSelectOption]
    @Binding var selection: [JSONValue]

    var body: some View {
        List {
            ForEach(options) { option in
                Button {
                    if let i = selection.firstIndex(of: option.value) { selection.remove(at: i) } else { selection.append(option.value) }
                } label: {
                    HStack {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(verbatim: option.name).foregroundStyle(Theme.text)
                            if let hint = option.hint { Text(verbatim: hint).font(.caption).foregroundStyle(Theme.textFaint) }
                        }
                        Spacer()
                        if selection.contains(option.value) { Image(systemName: "checkmark").foregroundStyle(Theme.accent) }
                    }
                }
            }
            if options.isEmpty { EmptyRow(text: "manage.empty") }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: title))
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// A row that opens a multi-select list and shows what is picked.
struct MultiSelectRow<Label: View>: View {
    let title: String
    let options: [ArrSelectOption]
    @Binding var selection: [JSONValue]
    @ViewBuilder let label: () -> Label

    var body: some View {
        NavigationLink {
            MultiSelectList(title: title, options: options, selection: $selection)
        } label: {
            LabeledContent {
                Text(verbatim: summary).lineLimit(1).foregroundStyle(Theme.textMuted)
            } label: {
                label()
            }
        }
    }

    private var summary: String {
        let names = selection.map { v in options.first { $0.value == v }?.name ?? v.text }
        return names.isEmpty ? "—" : names.joined(separator: ", ")
    }
}

/// Edits a list of free-form strings (terms, categories, required words).
struct StringListEditor: View {
    let title: String
    @Binding var values: [String]
    @State private var draft = ""

    var body: some View {
        List {
            Section {
                HStack {
                    TextField(text: $draft) { Text("manage.add_value") }
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                        .onSubmit(add)
                    Button(action: add) { Image(systemName: "plus.circle.fill") }
                        .disabled(draft.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
            Section {
                ForEach(Array(values.enumerated()), id: \.offset) { _, value in
                    Text(verbatim: value).font(.body.monospaced())
                }
                .onDelete { values.remove(atOffsets: $0) }
                if values.isEmpty { EmptyRow(text: "manage.empty") }
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: title))
        .navigationBarTitleDisplayMode(.inline)
    }

    private func add() {
        let v = draft.trimmingCharacters(in: .whitespaces)
        guard !v.isEmpty else { return }
        values.append(v)
        draft = ""
    }
}

/// A row that opens a string list editor.
struct StringListRow: View {
    let title: String
    @Binding var values: [String]

    var body: some View {
        NavigationLink {
            StringListEditor(title: title, values: $values)
        } label: {
            LabeledContent {
                Text(verbatim: values.isEmpty ? "—" : values.joined(separator: ", ")).lineLimit(1).foregroundStyle(Theme.textMuted)
            } label: {
                Text(verbatim: title)
            }
        }
    }
}

/// Servarr tags (`[{id, label}]`) as options.
func tagOptions(_ tags: [JSONValue]) -> [ArrSelectOption] {
    tags.map { ArrSelectOption(value: $0["id"], name: $0["label"].string ?? $0["id"].text, hint: nil) }
}

/// Binds a JSON array of strings as [String].
@MainActor
func stringArrayBinding(_ value: Binding<JSONValue>) -> Binding<[String]> {
    Binding(get: { value.wrappedValue.list.map(\.text) }, set: { value.wrappedValue = .array($0.map { .string($0) }) })
}

/// Binds a JSON array as [JSONValue].
@MainActor
func arrayBinding(_ value: Binding<JSONValue>) -> Binding<[JSONValue]> {
    Binding(get: { value.wrappedValue.list }, set: { value.wrappedValue = .array($0) })
}

/// A secret: never shows the stored value; empty means "keep what is saved".
struct SecretField: View {
    let title: String
    @Binding var text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: title).font(.caption).foregroundStyle(Theme.textMuted)
            SecureField(text: $text, prompt: Text("manage.secret_unchanged")) { Text(verbatim: title) }
                .textInputAutocapitalization(.never).autocorrectionDisabled()
        }
    }
}

// MARK: Schema fields

/// The fields of a schema-driven form (provider settings, custom format specifications).
struct SchemaFieldsView: View {
    @Binding var state: SchemaFormState
    var tags: [JSONValue] = []
    var failures: [ArrValidationFailure] = []
    var showAdvanced = false
    /// Loads options for fields that get them from the service.
    var loadOptions: (@MainActor (ArrField) async -> [ArrSelectOption])? = nil

    var body: some View {
        ForEach(state.fields.filter { $0.isVisible && (showAdvanced || !$0.advanced) }) { field in
            SchemaFieldRow(field: field, state: $state, tags: tags,
                           failures: failures.filter { $0.matches(field.name) }, loadOptions: loadOptions)
        }
    }
}

struct SchemaFieldRow: View {
    let field: ArrField
    @Binding var state: SchemaFormState
    let tags: [JSONValue]
    let failures: [ArrValidationFailure]
    var loadOptions: (@MainActor (ArrField) async -> [ArrSelectOption])?

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            control
            if let help = field.helpText {
                Text(verbatim: help).font(.caption).foregroundStyle(Theme.textFaint)
            }
            if let link = field.helpLink.flatMap(URL.init(string:)) {
                Link(destination: link) { Text("manage.more_info").font(.caption) }
            }
            ForEach(failures, id: \.self) { f in
                Text(verbatim: f.message).font(.caption).foregroundStyle(f.isWarning ? .orange : .red)
            }
        }
        .task(id: field.name) {
            if field.options.isEmpty, field.optionsAction != nil, let loadOptions {
                let loaded = await loadOptions(field)
                if !loaded.isEmpty { state.setOptions(field.name, loaded) }
            }
        }
    }

    private var title: String { field.unit.map { "\(field.label) (\($0))" } ?? field.label }

    private var binding: Binding<JSONValue> {
        Binding(get: { state.value(field.name) }, set: { state.set(field.name, $0) })
    }

    @ViewBuilder private var control: some View {
        if field.isSecret {
            SecretField(title: title, text: Binding(get: { state.secretText(field.name) }, set: { state.setSecret(field.name, $0) }))
        } else {
            switch field.kind {
            case .checkbox:
                Toggle(isOn: Binding(get: { binding.wrappedValue.bool ?? false }, set: { binding.wrappedValue = .bool($0) })) {
                    Text(verbatim: title)
                }
            case .number:
                LabeledContent {
                    JSONNumberField(value: binding, isFloat: field.isFloat)
                } label: { Text(verbatim: title) }
            case .select, .tagSelect:
                if field.isMultiSelect {
                    MultiSelectRow(title: field.label, options: field.options, selection: arrayBinding(binding)) { Text(verbatim: title) }
                } else {
                    Picker(selection: binding) {
                        if !field.options.contains(where: { $0.value == binding.wrappedValue }) {
                            Text(verbatim: binding.wrappedValue.text.isEmpty ? "—" : binding.wrappedValue.text).tag(binding.wrappedValue)
                        }
                        ForEach(field.options) { o in Text(verbatim: o.name).tag(o.value) }
                    } label: { Text(verbatim: title) }
                }
            case .tag:
                if field.name.lowercased() == "tags", binding.wrappedValue.list.allSatisfy(\.isNumber) {
                    MultiSelectRow(title: field.label, options: tagOptions(tags), selection: arrayBinding(binding)) { Text(verbatim: title) }
                } else {
                    StringListRow(title: title, values: stringArrayBinding(binding))
                }
            case .info:
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: field.label).font(.subheadline.weight(.semibold))
                    if !binding.wrappedValue.text.isEmpty {
                        Text(verbatim: binding.wrappedValue.text).font(.caption).foregroundStyle(Theme.textMuted)
                    }
                }
            case .hidden:
                EmptyView()
            case .action, .captcha, .oAuth, .device:
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: field.label).font(.subheadline.weight(.semibold))
                    Text("manage.field_unsupported").font(.caption).foregroundStyle(Theme.textFaint)
                }
            case .textArea:
                VStack(alignment: .leading, spacing: 4) {
                    Text(verbatim: title).font(.caption).foregroundStyle(Theme.textMuted)
                    TextField(text: textBinding, axis: .vertical) { Text(verbatim: title) }
                        .lineLimit(3...8)
                        .textInputAutocapitalization(.never).autocorrectionDisabled()
                }
            case .textbox, .path, .filePath, .url:
                textField
            case .keyValueList, .playlist, .unknown:
                FallbackValueEditor(title: title, value: binding)
            }
        }
    }

    private var textBinding: Binding<String> {
        Binding(get: { binding.wrappedValue.text }, set: { binding.wrappedValue = .string($0) })
    }

    private var textField: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(verbatim: title).font(.caption).foregroundStyle(Theme.textMuted)
            TextField(text: textBinding, prompt: field.placeholder.map { Text(verbatim: $0) }) { Text(verbatim: title) }
                .keyboardType(field.kind == .url ? .URL : .default)
                .textInputAutocapitalization(.never).autocorrectionDisabled()
                .font(field.kind == .path || field.kind == .filePath ? .body.monospaced() : .body)
        }
    }
}

/// Any value of a type the app does not know: edited by its JSON type, or shown read-only.
struct FallbackValueEditor: View {
    let title: String
    @Binding var value: JSONValue

    var body: some View {
        switch value {
        case .bool(let b):
            Toggle(isOn: Binding(get: { b }, set: { value = .bool($0) })) { Text(verbatim: title) }
        case .int, .double:
            LabeledContent { JSONNumberField(value: $value, isFloat: { if case .double = value { return true } else { return false } }()) } label: { Text(verbatim: title) }
        case .string, .null:
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: title).font(.caption).foregroundStyle(Theme.textMuted)
                TextField(text: Binding(get: { value.text }, set: { value = .string($0) })) { Text(verbatim: title) }
                    .textInputAutocapitalization(.never).autocorrectionDisabled()
            }
        case .array(let items) where items.allSatisfy({ $0.string != nil }):
            StringListRow(title: title, values: stringArrayBinding($value))
        default:
            VStack(alignment: .leading, spacing: 4) {
                Text(verbatim: title).font(.caption).foregroundStyle(Theme.textMuted)
                Text(verbatim: value.text).font(.caption.monospaced()).foregroundStyle(Theme.textFaint).lineLimit(4)
            }
        }
    }
}

// MARK: Generic object fields

/// Fields of a plain JSON object (config endpoints, Seerr and Bazarr settings), by dotted path.
struct ObjectFieldsView: View {
    @Binding var state: ObjectFormState
    let paths: [String]
    /// Known choices for string or number keys (by last path component or full path).
    var options: [String: [ArrSelectOption]] = [:]
    var tags: [JSONValue] = []
    var failures: [ArrValidationFailure] = []

    var body: some View {
        ForEach(paths, id: \.self) { path in
            VStack(alignment: .leading, spacing: 4) {
                ObjectFieldRow(path: path, state: $state, options: options[path] ?? options[lastKey(path)], tags: tags)
                ForEach(failures.filter { $0.matches(lastKey(path)) }, id: \.self) { f in
                    Text(verbatim: f.message).font(.caption).foregroundStyle(f.isWarning ? .orange : .red)
                }
            }
        }
    }

    private func lastKey(_ path: String) -> String { path.split(separator: ".").last.map(String.init) ?? path }
}

struct ObjectFieldRow: View {
    let path: String
    @Binding var state: ObjectFormState
    let options: [ArrSelectOption]?
    let tags: [JSONValue]

    private var title: String { path.humanizedKey }

    private var binding: Binding<JSONValue> {
        Binding(get: { state.value(path) }, set: { state.set(path, $0) })
    }

    var body: some View {
        if ObjectFormState.isSecretKey(path) {
            SecretField(title: title, text: Binding(get: { state.secretText(path) }, set: { state.setSecret(path, $0) }))
        } else if let options, !options.isEmpty {
            if binding.wrappedValue.array != nil {
                MultiSelectRow(title: title, options: options, selection: arrayBinding(binding)) { Text(verbatim: title) }
            } else {
                Picker(selection: binding) {
                    if !options.contains(where: { $0.value == binding.wrappedValue }) {
                        Text(verbatim: binding.wrappedValue.text.isEmpty ? "—" : binding.wrappedValue.text).tag(binding.wrappedValue)
                    }
                    ForEach(options) { o in Text(verbatim: o.name).tag(o.value) }
                } label: { Text(verbatim: title) }
            }
        } else if path.hasSuffix("tags"), binding.wrappedValue.list.allSatisfy(\.isNumber), !tags.isEmpty || binding.wrappedValue.list.isEmpty == false {
            MultiSelectRow(title: title, options: tagOptions(tags), selection: arrayBinding(binding)) { Text(verbatim: title) }
        } else if case .array(let items) = binding.wrappedValue, items.isEmpty || items.allSatisfy({ $0.string != nil }) {
            StringListRow(title: title, values: stringArrayBinding(binding))
        } else {
            FallbackValueEditor(title: title, value: binding)
        }
    }
}

/// Builds options from plain values ("none", "basic"): shown as the API names them.
func apiOptions(_ values: [String]) -> [ArrSelectOption] {
    values.map { ArrSelectOption(value: .string($0), name: $0.humanizedKey, hint: nil) }
}

/// Builds options from Servarr list resources (`[{id, name}]`), e.g. quality profiles.
func idOptions(_ items: [JSONValue], nameKey: String = "name") -> [ArrSelectOption] {
    items.map { ArrSelectOption(value: $0["id"], name: $0[nameKey].string ?? $0["id"].text, hint: nil) }
}
