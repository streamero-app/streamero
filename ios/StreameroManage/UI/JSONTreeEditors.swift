import SwiftUI

/// What a new element of a JSON array starts as: the shape of an existing element with its
/// values cleared (so a new object has the keys the service expects), or text for an empty list.
enum JSONArrayTemplate {
    static func blank(like sample: JSONValue) -> JSONValue {
        switch sample {
        case .null: return .null
        case .bool: return .bool(false)
        case .int: return .int(0)
        case .double: return .double(0)
        case .string: return .string("")
        case .array: return .array([])
        case .object(let o):
            var out: [String: JSONValue] = [:]
            for (k, v) in o where k != "id" { out[k] = blank(like: v) }
            return .object(out)
        }
    }

    static func newElement(for items: [JSONValue]) -> JSONValue {
        guard let sample = items.last else { return .string("") }
        return blank(like: sample)
    }

    /// A short label for an element row: its name-like field, else its position.
    static func label(_ item: JSONValue, index: Int) -> String {
        switch item {
        case .object:
            return item.firstString("name", "label", "title", "path", "key", "language", "value") ?? "#\(index + 1)"
        case .array(let a):
            return "#\(index + 1) (\(a.count))"
        default:
            return item.text.isEmpty ? "#\(index + 1)" : item.text
        }
    }

    static func moving(_ items: [JSONValue], from source: IndexSet, to destination: Int) -> [JSONValue] {
        var list = items
        let moving = source.sorted().map { list[$0] }
        for i in source.sorted(by: >) { list.remove(at: i) }
        let target = destination - source.filter { $0 < destination }.count
        list.insert(contentsOf: moving, at: max(0, min(target, list.count)))
        return list
    }
}

/// One value of any JSON type: scalars edited in place, objects and arrays opened on their
/// own screen (where array elements can be added, removed and reordered).
struct JSONTreeRow: View {
    let title: String
    @Binding var value: JSONValue

    var body: some View {
        switch value {
        case .object(let o):
            NavigationLink {
                JSONObjectEditor(title: title, value: $value)
            } label: {
                LabeledContent {
                    Text("manage.json_keys \(o.count)").foregroundStyle(Theme.textMuted)
                } label: { Text(verbatim: title) }
            }
        case .array(let items):
            NavigationLink {
                JSONArrayEditor(title: title, value: $value)
            } label: {
                LabeledContent {
                    Text("manage.json_items \(items.count)").foregroundStyle(Theme.textMuted)
                } label: { Text(verbatim: title) }
            }
        default:
            FallbackValueEditor(title: title, value: $value)
        }
    }
}

/// Every key of a JSON object (an element of a nested list).
struct JSONObjectEditor: View {
    let title: String
    @Binding var value: JSONValue

    var body: some View {
        Form {
            Section {
                ForEach((value.object ?? [:]).keys.sorted(), id: \.self) { key in
                    JSONTreeRow(title: key.humanizedKey, value: Binding(get: { value[key] }, set: { value[key] = $0 }))
                }
                if (value.object ?? [:]).isEmpty { EmptyRow(text: "manage.empty") }
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: title))
        .navigationBarTitleDisplayMode(.inline)
    }
}

/// The elements of a JSON array: add, remove (swipe) and reorder (Edit).
struct JSONArrayEditor: View {
    let title: String
    @Binding var value: JSONValue

    private var items: [JSONValue] { value.list }

    var body: some View {
        List {
            Section {
                ForEach(Array(items.enumerated()), id: \.offset) { index, item in
                    JSONTreeRow(title: JSONArrayTemplate.label(item, index: index), value: element(index))
                }
                .onDelete { offsets in
                    var list = items
                    list.remove(atOffsets: offsets)
                    value = .array(list)
                }
                .onMove { source, destination in
                    value = .array(JSONArrayTemplate.moving(items, from: source, to: destination))
                }
                if items.isEmpty { EmptyRow(text: "manage.empty") }
            }
            Section {
                Button {
                    withAnimation(.snappy) { value = .array(items + [JSONArrayTemplate.newElement(for: items)]) }
                } label: {
                    Label("manage.add_item", systemImage: "plus.circle.fill")
                }
            }
        }
        .manageBackground()
        .navigationTitle(Text(verbatim: title))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            if items.count > 1 { ToolbarItem(placement: .primaryAction) { EditButton() } }
        }
    }

    private func element(_ index: Int) -> Binding<JSONValue> {
        Binding(get: { value[index] }, set: { new in
            var list = value.list
            guard list.indices.contains(index) else { return }
            list[index] = new
            value = .array(list)
        })
    }
}
