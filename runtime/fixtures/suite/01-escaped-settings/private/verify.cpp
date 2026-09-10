#include <cstdlib>
#include <iostream>
#include <stdexcept>
#include <string>
#define CHECK(x) do { if (!(x)) { std::cerr << "FAIL line " << __LINE__ << ": " << #x << '\n'; return 1; } } while (0)
template<class F> bool invalid(F fn) { try { fn(); } catch (const std::invalid_argument&) { return true; } return false; }
#include "settings.hpp"
int main() {
    CHECK(parse_pairs("").empty());
    CHECK(parse_pairs("x=1;y=2").at("y") == "2");
    CHECK(parse_pairs(R"(a=1\;2)").at("a") == "1;2");
    CHECK(parse_pairs(R"(\;\==\;\=)").at(";=") == ";=");
    CHECK(parse_pairs(R"(slash=\\;eq=a=b)").at("slash") == "\\");
    CHECK(parse_pairs(" spaced =  ").at(" spaced ") == "  ");
    CHECK(parse_pairs("empty=").at("empty").empty());
    for (auto s : {"=a", "a", "a=1;a=2", "a=1;", ";a=1", "a=1;;b=2", "a=\\q", "a=\\"}) CHECK(invalid([&] { parse_pairs(s); }));
    for (int i=0; i<100; ++i) {
        std::string v=std::to_string(i)+";=\\end", encoded;
        for(char c:v) { if(c==';'||c=='='||c=='\\') encoded+='\\'; encoded+=c; }
        CHECK(parse_pairs("k="+encoded).at("k")==v);
    }
    std::cout << "PASS escaped-settings: 115 cases\n";
}
