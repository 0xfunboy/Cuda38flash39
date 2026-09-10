#pragma once
#include <cstdint>
#include <map>
#include <string>
class Catalog {
    std::map<std::string,std::int64_t> prices_;
    std::uint64_t revision_=0;
public:
    void set_price(const std::string& sku,std::int64_t cents);
    std::int64_t price(const std::string& sku) const;
    std::uint64_t revision() const { return revision_; }
};
