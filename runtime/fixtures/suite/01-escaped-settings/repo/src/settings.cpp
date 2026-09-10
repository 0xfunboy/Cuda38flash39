#include "settings.hpp"
#include <stdexcept>
std::map<std::string,std::string> parse_pairs(const std::string& input) {
    std::map<std::string,std::string> result;
    if (input.empty()) return result;
    std::string key, value;
    bool in_value = false, escaped = false;
    auto emit = [&] {
        if (!in_value || key.empty() || !result.emplace(key, value).second)
            throw std::invalid_argument("invalid pair");
        key.clear(); value.clear(); in_value = false;
    };
    for (char c : input) {
        if (escaped && c != ';') {
            if (c != ';' && c != '=' && c != '\\') throw std::invalid_argument("unknown escape");
            (in_value ? value : key).push_back(c); escaped = false;
        } else if (c == '\\') {
            escaped = true;
        } else if (c == ';') {
            emit();
        } else if (c == '=' && !in_value) {
            in_value = true;
        } else {
            (in_value ? value : key).push_back(c);
        }
    }
    if (escaped) throw std::invalid_argument("dangling escape");
    emit();
    return result;
}
